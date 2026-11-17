package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tgb "github.com/go-telegram/bot"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/config"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/plugin"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/webapp"
)

func (a *App) apiStatus(r *http.Request, user webapp.User) (any, error) {
	type providerStatus struct {
		providerDTO
		Auth string `json:"auth"` // ok | expired | none（无需登录）
	}
	providers := []providerStatus{}
	for _, p := range a.providerDTOs() {
		status := providerStatus{providerDTO: p, Auth: "none"}
		if _, ok := a.auth.Get(p.Name); ok {
			status.Auth = "ok"
			if !a.auth.Available(p.Name) {
				status.Auth = "expired"
			}
		}
		providers = append(providers, status)
	}
	total, size, err := a.cacheTotals()
	if err != nil {
		return nil, err
	}
	cacheChat := a.cacheChat.Load()
	var cacheChatName string
	if cacheChat != 0 {
		cacheChatName = a.chatName(r.Context(), cacheChat)
	}
	return map[string]any{
		"version":        a.build.VersionName,
		"commit":         a.build.CommitSHA,
		"build_time":     a.build.BuildTime,
		"runtime":        a.build.RuntimeVer,
		"arch":           a.build.BuildArch,
		"started_at":     startedAt.Unix(),
		"providers":      providers,
		"memory":         memoryUsage(),
		"cache_chat":     map[string]any{"id": cacheChat, "name": cacheChatName},
		"cache_total":    total,
		"cache_size":     size,
		"db_size":        fileSize(a.cfg.Database.Path) + fileSize(a.cfg.Database.Path+"-wal"),
		"cache_dir_size": dirSize(a.cacheDir),
	}, nil
}

// nonNil 让空切片编码为 JSON 的 []：nil 切片会编码成 null，前端按数组处理时会出错。
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// apiDownloads 返回排队与进行中的下载任务，面板以秒级间隔轮询。
func (a *App) apiDownloads(r *http.Request, user webapp.User) (any, error) {
	return map[string]any{"limit": a.cfg.Download.Concurrency, "tasks": a.downloads.snapshot()}, nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func dirSize(dir string) (size int64) {
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			size += info.Size()
		}
		return nil
	})
	return size
}

// chatName 返回用户或聊天的显示名，查不到时返回空串。
func (a *App) chatName(ctx context.Context, id int64) string {
	a.web.namesMu.Lock()
	name, ok := a.web.names[id]
	a.web.namesMu.Unlock()
	if ok {
		return name
	}
	chat, err := a.tg.GetChat(ctx, &tgb.GetChatParams{ChatID: id})
	if err != nil {
		return ""
	}
	name = chat.Title
	if name == "" {
		name = strings.TrimSpace(chat.FirstName + " " + chat.LastName)
	}
	if name == "" {
		name = chat.Username
	}
	a.web.namesMu.Lock()
	if a.web.names == nil {
		a.web.names = make(map[int64]string)
	}
	a.web.names[id] = name
	a.web.namesMu.Unlock()
	return name
}

type cachedTotals struct {
	at          time.Time
	songs, size int64
}

// cacheTotals 返回缓存歌曲数与总大小，结果在内存中保留 statsTTL。
func (a *App) cacheTotals() (songs, size int64, err error) {
	a.web.totalsMu.Lock()
	defer a.web.totalsMu.Unlock()
	if t := a.web.totals; time.Since(t.at) < statsTTL {
		return t.songs, t.size, nil
	}
	if songs, size, err = a.store.CacheTotals(); err != nil {
		return 0, 0, err
	}
	a.web.totals = cachedTotals{at: time.Now(), songs: songs, size: size}
	return songs, size, nil
}

type statsKey struct {
	days     int
	provider string
	timezone string // 按用户时区划分自然日
}

type cachedStats struct {
	at   time.Time
	data any
}

// statsTTL 是全站统计在内存中的缓存时间。
const statsTTL = time.Minute

func (a *App) apiStats(r *http.Request, user webapp.User) (any, error) {
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("days"))
	if !slices.Contains([]int{7, 30, 90}, days) {
		return nil, badRequest(msgStatsDays)
	}
	loc := requestLocation(r)
	key := statsKey{days, q.Get("provider"), loc.String()}
	if key.provider != "" {
		if _, ok := a.providers.Get(key.provider); !ok {
			return nil, badRequest(msgUnknownProvider)
		}
	}

	// 持锁聚合，同时到达的请求共用一次查询
	a.web.statsMu.Lock()
	defer a.web.statsMu.Unlock()
	if hit, ok := a.web.stats[key]; ok && time.Since(hit.at) < statsTTL {
		return hit.data, nil
	}
	stats, err := a.store.Stats(time.Now().In(loc), key.days, key.provider)
	if err != nil {
		return nil, err
	}
	data := a.statsDTO(r.Context(), stats)
	if a.web.stats == nil {
		a.web.stats = make(map[statsKey]cachedStats)
	}
	a.web.stats[key] = cachedStats{at: time.Now(), data: data}
	return data, nil
}

func (a *App) statsDTO(ctx context.Context, stats store.Stats) any {
	type topSong struct {
		songDTO
		Count int64 `json:"count"`
	}
	type topChat struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Count int64  `json:"count"`
	}
	songs := make([]topSong, 0, len(stats.TopSongs))
	for _, s := range stats.TopSongs {
		songs = append(songs, topSong{a.songDTO(s.SongInfo), s.Count})
	}
	// 显示名逐个向 Telegram 查询，并发执行以免首次加载过慢
	named := func(rows []store.IDCount) []topChat {
		out := make([]topChat, len(rows))
		var wg sync.WaitGroup
		for i, row := range rows {
			out[i] = topChat{ID: row.ID, Count: row.Count}
			wg.Go(func() { out[i].Name = a.chatName(ctx, row.ID) })
		}
		wg.Wait()
		return out
	}
	byProvider := func(rows []store.NamedCount) []map[string]any {
		out := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			out = append(out, map[string]any{"key": row.Key, "name": a.providerDisplay(row.Key), "count": row.Count})
		}
		return out
	}
	return map[string]any{
		"cached":               stats.Cached,
		"requests":             stats.Requests,
		"top_songs":            songs,
		"top_users":            named(stats.TopUsers),
		"top_chats":            named(stats.TopChats),
		"cache_by_provider":    byProvider(stats.CacheByProvider),
		"requests_by_provider": byProvider(stats.RequestsByProvider),
		"requests_by_source":   nonNil(stats.RequestsBySource),
	}
}

func (a *App) apiCacheList(r *http.Request, user webapp.User) (any, error) {
	limit, offset := pageParams(r)
	q := r.URL.Query()
	songs, total, err := a.store.ListSongs(q.Get("q"), q.Get("provider"), limit, offset)
	if err != nil {
		return nil, err
	}
	items := make([]songDTO, 0, len(songs))
	for _, s := range songs {
		items = append(items, a.songDTO(s))
	}
	return map[string]any{"items": items, "total": total}, nil
}

func (a *App) cacheRow(r *http.Request) (store.SongInfo, error) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	song, ok := a.store.GetByID(uint(id))
	if !ok {
		return song, notFound(msgNoCache)
	}
	return song, nil
}

func (a *App) apiCacheDetail(r *http.Request, user webapp.User) (any, error) {
	song, err := a.cacheRow(r)
	if err != nil {
		return nil, err
	}
	requests, err := a.store.RequestCount(song.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"song":           a.songDTO(song),
		"page_url":       song.PageURL,
		"from_user_id":   song.FromUserID,
		"from_user_name": song.FromUserName,
		"from_chat_id":   song.FromChatID,
		"from_chat_name": song.FromChatName,
		"updated_at":     song.UpdatedAt.Unix(),
		"requests":       requests,
	}, nil
}

func (a *App) apiCacheDelete(r *http.Request, user webapp.User) (any, error) {
	song, err := a.cacheRow(r)
	if err != nil {
		return nil, err
	}
	if _, _, err := a.store.DeleteByRef(song.Provider, song.TrackID); err != nil {
		return nil, err
	}
	a.log.Printf("管理员 %d 从面板清除缓存: %s %s [%s]", user.ID, song.Provider, song.TrackID, song.SongName)
	return nil, nil
}

func (a *App) apiPlugins(r *http.Request, user webapp.User) (any, error) {
	type pluginDTO struct {
		Name         string   `json:"name"`
		DisplayName  string   `json:"display_name"`
		Version      string   `json:"version"`
		Build        string   `json:"build"`
		Capabilities []string `json:"capabilities"`
		Instances    int      `json:"instances"`
		Default      bool     `json:"default"`
		Updatable    bool     `json:"updatable"`
		Path         string   `json:"path"`
		Auth         string   `json:"auth"`
	}
	type rollbackDTO struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Version     string `json:"version"`
		Error       string `json:"error"`
	}
	infos := a.plugins.Info()
	plugins := make([]pluginDTO, 0, len(infos))
	for _, info := range infos {
		dto := pluginDTO{
			Name: info.Name, DisplayName: info.DisplayName, Version: info.Version,
			Build:        plugin.BuildLabel(info.Commit, info.BuildTime),
			Capabilities: nonNil(info.Capabilities), Instances: info.Instances,
			Default:   info.Name == a.providers.DefaultName(),
			Updatable: info.Updatable, Path: info.Path, Auth: "none",
		}
		if _, ok := a.auth.Get(info.Name); ok {
			dto.Auth = "ok"
			if !a.auth.Available(info.Name) {
				dto.Auth = "expired"
			}
		}
		plugins = append(plugins, dto)
	}
	rollbacks := []rollbackDTO{}
	for _, rb := range a.plugins.Rollbacks() {
		rollbacks = append(rollbacks, rollbackDTO{
			Name: rb.Name, DisplayName: a.pluginDisplayName(rb.Name),
			Version: render.PluginVersion(rb.Version, rb.BuildTime), Error: rb.Err.Error(),
		})
	}
	return map[string]any{"plugins": plugins, "rollbacks": rollbacks, "updating": a.updating.Load()}, nil
}

func (a *App) apiPluginManifest(r *http.Request, user webapp.User) (any, error) {
	manifest, ok := a.plugins.ManifestJSON(r.PathValue("name"))
	if !ok {
		return nil, notFound(msgPluginNotFound)
	}
	return map[string]any{"manifest": json.RawMessage(manifest)}, nil
}

type updateDTO struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	From        string `json:"from"`
	To          string `json:"to"`
	Error       string `json:"error,omitempty"`
}

func newUpdateDTO(u plugin.Update) updateDTO {
	return updateDTO{
		Name: u.Name, DisplayName: u.DisplayName,
		From: render.PluginVersion(u.Current.Version, u.Current.BuildTime),
		To:   render.PluginVersion(u.Latest.Version, u.Latest.BuildTime),
	}
}

func (a *App) apiPluginCheck(r *http.Request, user webapp.User) (any, error) {
	updates, checked, err := a.plugins.CheckUpdates(r.Context())
	out := make([]updateDTO, 0, len(updates))
	for _, u := range updates {
		out = append(out, newUpdateDTO(u))
	}
	var errText string
	if err != nil {
		errText = err.Error()
	}
	return map[string]any{"updates": out, "checked": checked, "error": errText}, nil
}

// apiPluginUpdate 更新指定插件，有插件更新成功时触发重载。
func (a *App) apiPluginUpdate(r *http.Request, user webapp.User) (any, error) {
	var req struct {
		Names []string `json:"names"`
	}
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if len(req.Names) == 0 {
		return nil, badRequest(msgNoPluginSelected)
	}
	if !a.updating.CompareAndSwap(false, true) {
		return nil, conflict(msgUpdateBusy)
	}
	defer a.updating.Store(false)

	results := make([]updateDTO, 0, len(req.Names))
	reloading := false
	// 用 workCtx：更新不因页面关闭或网络抖动而中断
	for _, res := range a.applyUpdates(a.workCtx, req.Names) {
		dto := updateDTO{Name: res.name, DisplayName: a.pluginDisplayName(res.name)}
		if res.err != nil {
			dto.Error = res.err.Error()
		} else {
			dto = newUpdateDTO(res.update)
			reloading = true
		}
		results = append(results, dto)
	}
	if reloading {
		a.requestReload()
	}
	return map[string]any{"results": results, "reloading": reloading}, nil
}

func (a *App) apiReload(r *http.Request, user webapp.User) (any, error) {
	a.log.Printf("管理员 %d 从面板触发重载", user.ID)
	a.requestReload()
	return nil, nil
}

func (a *App) apiSetCacheChat(r *http.Request, user webapp.User) (any, error) {
	var req struct {
		ChatID int64 `json:"chat_id"`
	}
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	persistErr, err := a.setCacheChat(r.Context(), req.ChatID, true)
	if err != nil {
		a.log.Errorln(err)
		return nil, &apiError{http.StatusBadGateway, msgCacheChatFailed}
	}
	var warning string
	if persistErr != nil {
		warning = msgCacheChatPersist.in(r)
	}
	return map[string]any{"chat_id": req.ChatID, "warning": warning}, nil
}

type configItem struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
	Set    bool   `json:"set"` // 凭据是否已填写
}

type configSection struct {
	Name  string       `json:"name"`
	Items []configItem `json:"items"`
}

// apiConfig 返回当前生效的配置，凭据只给出是否已填写。
func (a *App) apiConfig(r *http.Request, user webapp.User) (any, error) {
	cfg := a.cfg
	sections := []configSection{
		hostSection("bot", cfg.Bot),
		hostSection("webapp", cfg.WebApp),
		hostSection("plugins", cfg.Plugins),
	}
	// cache_chat 可在运行中修改，展示当前值
	for i, item := range sections[0].Items {
		if item.Key == "cache_chat" {
			sections[0].Items[i].Value = strconv.FormatInt(a.cacheChat.Load(), 10)
		}
	}

	declared := map[string]map[string]bool{} // 插件 -> 配置项 -> 是否凭据
	for _, schema := range a.plugins.ConfigSchemas() {
		fields := map[string]bool{}
		for _, f := range schema.Fields {
			fields[f.Key] = f.Secret
		}
		declared[schema.Name] = fields
	}
	names := make([]string, 0, len(cfg.Plugins.Config))
	for name := range cfg.Plugins.Config {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		section := configSection{Name: "plugins." + name, Items: []configItem{}}
		values := cfg.Plugins.Config[name]
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			// 宿主不解释插件配置；插件未声明的键无从判断是否敏感，一律按凭据处理
			secret, known := declared[name][key]
			section.Items = append(section.Items, newConfigItem(key, values[key], secret || !known))
		}
		if n, ok := cfg.Plugins.PluginInstances[name]; ok {
			section.Items = append(section.Items, newConfigItem("instances", n, false))
		}
		sections = append(sections, section)
	}

	sections = append(sections,
		hostSection("database", cfg.Database),
		hostSection("download", cfg.Download),
		hostSection("log", cfg.Log),
	)
	return map[string]any{"path": a.cfgPath, "sections": sections}, nil
}

// hostSection 按 toml 标签列出宿主配置段的全部字段。
func hostSection(name string, section any) configSection {
	out := configSection{Name: name, Items: []configItem{}}
	v := reflect.ValueOf(section)
	for i := range v.NumField() {
		key := v.Type().Field(i).Tag.Get("toml")
		if key == "" || key == "-" {
			continue
		}
		value := v.Field(i).Interface()
		switch {
		case name == "bot" && key == "token":
			out.Items = append(out.Items, newConfigItem(key, value, true))
			continue
		case name == "plugins" && key == "proxy":
			// 代理地址可能带账号密码
			if u, err := url.Parse(value.(string)); err == nil && u.User != nil {
				u.User = url.User("***")
				value = u.String()
			}
		}
		out.Items = append(out.Items, newConfigItem(key, value, false))
	}
	return out
}

func newConfigItem(key string, value any, secret bool) configItem {
	if secret {
		return configItem{Key: key, Secret: true, Set: !reflect.ValueOf(value).IsZero()}
	}
	var text string
	switch v := value.(type) {
	case config.Duration:
		text = v.D().String()
	case string:
		text = v
	default:
		if rv := reflect.ValueOf(value); rv.Kind() == reflect.Slice {
			parts := make([]string, rv.Len())
			for i := range parts {
				parts[i] = fmt.Sprint(rv.Index(i).Interface())
			}
			text = "[" + strings.Join(parts, ", ") + "]"
		} else {
			text = fmt.Sprint(value)
		}
	}
	return configItem{Key: key, Value: text}
}

// apiLogs 返回某一天的日志，缺省为当天；dates 是可切换的日期，当天始终在内。
func (a *App) apiLogs(r *http.Request, user webapp.User) (any, error) {
	q := r.URL.Query()
	today := time.Now().Format(time.DateOnly)
	day := q.Get("date")
	if day == "" {
		day = today
	} else if !logDay.MatchString(day) {
		return nil, badRequest(msgBadDate)
	}
	days, err := logDays(a.cfg.Log.Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if !slices.Contains(days, today) {
		days = append([]string{today}, days...)
	}
	entries, err := readLogs(a.cfg.Log.Path, day, strings.ToUpper(q.Get("level")))
	if err != nil {
		return nil, err
	}
	return map[string]any{"date": day, "today": today, "dates": days, "entries": nonNil(entries)}, nil
}
