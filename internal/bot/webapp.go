package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/webapp"
)

// webState 是面板独有的运行时状态。
type webState struct {
	guard   *webGuard
	statsMu sync.Mutex
	stats   map[statsKey]cachedStats
	// 缓存总数与总大小：管理首页每 10 秒刷新一次，这两项不需要这么新
	totalsMu sync.Mutex
	totals   cachedTotals
	namesMu  sync.Mutex
	names    map[int64]string // 用户/聊天 ID -> 显示名，仅缓存查询成功的结果
	loginMu  sync.Mutex       // 保护 login
	login    *webLogin
}

// startWebApp 监听 [webapp] listen 并在后台提供面板服务，返回的函数停止接收新请求并等待
// 在途请求结束，最多等待 timeout。baseCtx 在进程退出时取消。
func (a *App) startWebApp(baseCtx context.Context) (stop func(timeout time.Duration), err error) {
	ln, err := net.Listen("tcp", a.cfg.WebApp.Listen)
	if err != nil {
		return nil, err
	}
	if !webapp.Built() {
		a.log.Warnln("二进制未包含面板前端，请用 build.sh 构建；面板页面暂不可用")
	}
	if host, _, _ := net.SplitHostPort(a.cfg.WebApp.Listen); host != "localhost" {
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			a.log.Warnf("面板服务监听在 %s，本机以外也能直接访问；建议只监听 127.0.0.1，由 cloudflared 或反向代理转发", a.cfg.WebApp.Listen)
		}
	}
	a.web.guard = newWebGuard()
	srv := &http.Server{
		Handler:           a.guard(a.webHandler()),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      apiTimeout + writeGrace,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    maxHeaderBytes,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.log.Errorf("面板服务异常退出: %v", err)
		}
	}()
	a.log.Printf("面板服务已启动: %s -> %s", a.cfg.WebApp.Listen, a.cfg.WebApp.URL)
	return func(timeout time.Duration) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			a.log.Warnln("等待面板请求结束超时，强制关闭")
			_ = srv.Close()
		}
	}, nil
}

// setupWebAppEntry 把 bot 的 Menu Button 设为打开面板。
func (a *App) setupWebAppEntry(ctx context.Context) {
	if _, err := a.tg.SetChatMenuButton(ctx, &tgb.SetChatMenuButtonParams{
		MenuButton: models.MenuButtonWebApp{
			Type:   models.MenuButtonTypeWebApp,
			Text:   render.WebAppButton,
			WebApp: models.WebAppInfo{URL: a.cfg.WebApp.URL},
		},
	}); err != nil {
		a.log.Warnf("设置 Menu Button 失败: %v", err)
	}
}

func webAppKeyboard(url string) models.InlineKeyboardMarkup {
	return models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{
		Text:   render.WebAppOpen,
		WebApp: &models.WebAppInfo{URL: url},
	}}}}
}

func (a *App) webHandler() http.Handler {
	mux := http.NewServeMux()
	user := func(pattern string, fn apiFunc) { mux.Handle(pattern, a.api(false, apiTimeout, fn)) }
	admin := func(pattern string, fn apiFunc) { mux.Handle(pattern, a.api(true, apiTimeout, fn)) }

	user("GET /api/me", a.apiMe)
	user("GET /api/overview", a.apiOverview)
	user("GET /api/songs", a.apiSongs)
	user("GET /api/search", a.apiSearch)
	user("POST /api/music", a.apiMusic)

	admin("GET /api/admin/status", a.apiStatus)
	admin("GET /api/admin/downloads", a.apiDownloads)
	admin("GET /api/admin/stats", a.apiStats)
	admin("GET /api/admin/cache", a.apiCacheList)
	admin("GET /api/admin/cache/{id}", a.apiCacheDetail)
	admin("DELETE /api/admin/cache/{id}", a.apiCacheDelete)
	admin("GET /api/admin/login", a.apiLoginState)
	admin("POST /api/admin/login", a.apiLoginBegin)
	admin("POST /api/admin/login/token", a.apiLoginToken)
	admin("DELETE /api/admin/login", a.apiLoginCancel)
	admin("GET /api/admin/plugins", a.apiPlugins)
	admin("GET /api/admin/plugins/{name}/manifest", a.apiPluginManifest)
	// 检查与应用更新要下载文件，使用各自的时限
	mux.Handle("POST /api/admin/plugins/check", a.api(true, updateCheckTimeout, a.apiPluginCheck))
	mux.Handle("POST /api/admin/plugins/update", a.api(true, updateApplyTimeout, a.apiPluginUpdate))
	admin("GET /api/admin/config", a.apiConfig)
	admin("POST /api/admin/cache-chat", a.apiSetCacheChat)
	admin("POST /api/admin/reload", a.apiReload)
	admin("GET /api/admin/logs", a.apiLogs)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, msgNotFoundAPI)
	})
	mux.Handle("/", webapp.Static())
	return mux
}

// apiFunc 处理一个已通过身份校验的请求，返回值编码为 JSON。
type apiFunc func(r *http.Request, user webapp.User) (any, error)

// apiError 是返回给前端的错误，Text 直接展示给用户。
type apiError struct {
	Status int
	Text   webText
}

func (e *apiError) Error() string { return e.Text.zh }

func badRequest(text webText) error { return &apiError{http.StatusBadRequest, text} }
func notFound(text webText) error   { return &apiError{http.StatusNotFound, text} }
func conflict(text webText) error   { return &apiError{http.StatusConflict, text} }

const (
	initDataScheme  = "tma "
	maxRequestBytes = 64 << 10
	// initData 约 1KB，请求头整体不会超过这个大小
	maxHeaderBytes = 16 << 10

	serverReadTimeout = 15 * time.Second
	// apiTimeout 是普通接口的处理时限，长于默认的插件调用超时
	apiTimeout = 45 * time.Second
	// writeGrace 是处理时限之外留给写回响应的时间
	writeGrace = 15 * time.Second
)

// api 校验 initData 并按用户限流后调用 fn，fn 须在 timeout 内完成；
// adminOnly 的接口还要求用户在 [bot] admin 中。
func (a *App) api(adminOnly bool, timeout time.Duration, fn apiFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				a.log.Errorf("panic: %v\n%s", rec, debug.Stack())
				writeError(w, r, http.StatusInternalServerError, msgInternal)
			}
		}()
		w.Header().Set("Cache-Control", "no-store")
		initData, ok := strings.CutPrefix(r.Header.Get("Authorization"), initDataScheme)
		if !ok {
			writeError(w, r, http.StatusUnauthorized, msgNotInTelegram)
			return
		}
		user, err := webapp.ValidateInitData(initData, a.cfg.Bot.Token, time.Now())
		if err != nil {
			writeError(w, r, http.StatusUnauthorized, msgAuthFailed)
			return
		}
		if ok, retryAfter := a.web.guard.byUser.allow(user.ID, time.Now()); !ok {
			tooManyRequests(w, r, retryAfter)
			return
		}
		if adminOnly && !a.isAdmin(user.ID) {
			writeError(w, r, http.StatusForbidden, msgAdminOnly)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		r = r.WithContext(ctx)
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(timeout + writeGrace))
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		result, err := fn(r, user)
		if err != nil {
			var apiErr *apiError
			var limited *rateLimitError
			switch {
			case errors.As(err, &limited):
				tooManyRequests(w, r, limited.retryAfter)
				return
			case !errors.As(err, &apiErr):
				a.log.Errorf("面板 %s %s: %v", r.Method, r.URL.Path, err)
				apiErr = &apiError{http.StatusInternalServerError, msgInternal}
			}
			writeError(w, r, apiErr.Status, apiErr.Text)
			return
		}
		if result == nil {
			result = struct{}{}
		}
		writeJSON(w, http.StatusOK, result)
	})
}

// rateLimitError 表示接口自身的限流未通过。
type rateLimitError struct{ retryAfter time.Duration }

func (e *rateLimitError) Error() string { return "rate limited" }

// requestLocation 返回前端在 X-Timezone 请求头中给出的用户时区（IANA 名称），统计按它划分自然日；
// 没有该请求头或无法识别时使用服务器时区。
func requestLocation(r *http.Request) *time.Location {
	if name := r.Header.Get("X-Timezone"); name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return time.Local
}

func (a *App) isAdmin(userID int64) bool { return slices.Contains(a.cfg.Bot.Admin, userID) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeBody(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return badRequest(msgBadRequest)
	}
	return nil
}

const maxPageSize = 100

// pageParams 解析 limit 与 offset；limit 缺省或超出范围时取 maxPageSize。
func pageParams(r *http.Request) (limit, offset int) {
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > maxPageSize {
		limit = maxPageSize
	}
	offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	return limit, max(offset, 0)
}

// songDTO 是面板展示的缓存歌曲。
type songDTO struct {
	ID           uint   `json:"id"`
	Provider     string `json:"provider"`
	ProviderName string `json:"provider_name"`
	TrackID      string `json:"track_id"`
	Title        string `json:"title"`
	Artists      string `json:"artists"`
	Album        string `json:"album"`
	Ext          string `json:"ext"`
	Size         int    `json:"size"`
	BitRate      int    `json:"bit_rate"`
	Duration     int    `json:"duration"`
	CachedAt     int64  `json:"cached_at"`
}

func (a *App) songDTO(s store.SongInfo) songDTO {
	name := s.ProviderName
	if name == "" {
		name = a.providerDisplay(s.Provider)
	}
	return songDTO{
		ID: s.ID, Provider: s.Provider, ProviderName: name, TrackID: s.TrackID,
		Title: s.SongName, Artists: s.SongArtists, Album: s.SongAlbum, Ext: s.FileExt,
		Size: s.MusicSize + s.EmbPicSize, BitRate: s.BitRate, Duration: s.Duration,
		CachedAt: s.CreatedAt.Unix(),
	}
}

// goInflight 在后台执行 fn 并登记为在途任务，重载与退出时等它结束。
// 只能在 HTTP 请求处理中调用：面板服务先于在途任务的等待停止，不会与 Wait 竞争。
func (a *App) goInflight(fn func(ctx context.Context)) {
	a.inflight.Add(1)
	go func() {
		defer a.inflight.Done()
		defer func() {
			if r := recover(); r != nil {
				a.log.Errorf("panic: %v\n%s", r, debug.Stack())
			}
		}()
		fn(a.workCtx)
	}()
}
