package bot

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/textx"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/webapp"
)

// 前端传入的文本长度上限：搜索关键词与歌曲引用都会进入数据库查询或插件调用
const (
	maxKeywordRunes = 100
	maxRefBytes     = 256
)

type providerDTO struct {
	Name    string `json:"name"`
	Display string `json:"display"`
	Default bool   `json:"default"`
}

func (a *App) providerDTOs() []providerDTO {
	enabled := a.providers.Enabled()
	out := make([]providerDTO, 0, len(enabled))
	for _, p := range enabled {
		out = append(out, providerDTO{Name: p.Name(), Display: p.DisplayName(), Default: p.Name() == a.providers.DefaultName()})
	}
	return out
}

func (a *App) apiMe(r *http.Request, user webapp.User) (any, error) {
	return map[string]any{
		"id":        user.ID,
		"name":      strings.TrimSpace(user.FirstName + " " + user.LastName),
		"admin":     a.isAdmin(user.ID),
		"bot":       a.me.Username,
		"providers": a.providerDTOs(),
	}, nil
}

func (a *App) apiOverview(r *http.Request, user webapp.User) (any, error) {
	summary, err := a.store.UserSummary(user.ID)
	if err != nil {
		return nil, err
	}
	total, _, err := a.store.CacheTotals()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"songs":        summary.Songs,
		"duration":     summary.Duration,
		"size":         summary.Size,
		"total_cached": total,
	}, nil
}

func (a *App) apiSongs(r *http.Request, user webapp.User) (any, error) {
	limit, offset := pageParams(r)
	q := r.URL.Query()
	if utf8.RuneCountInString(q.Get("q")) > maxKeywordRunes {
		return nil, badRequest(msgKeywordTooLong)
	}
	songs, total, err := a.store.UserSongs(user.ID, q.Get("q"), store.SongOrder(q.Get("order")), limit, offset)
	if err != nil {
		return nil, err
	}
	type item struct {
		songDTO
		LastAt int64 `json:"last_at"`
		Times  int64 `json:"times"`
	}
	items := make([]item, 0, len(songs))
	for _, s := range songs {
		items = append(items, item{a.songDTO(s.SongInfo), s.LastAt, s.Times})
	}
	return map[string]any{"items": items, "total": total}, nil
}

// refDTO 是前端回传的歌曲引用。
type refDTO struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Sub      string `json:"sub,omitempty"`
}

func (a *App) apiSearch(r *http.Request, user webapp.User) (any, error) {
	q := r.URL.Query()
	keyword := strings.TrimSpace(q.Get("q"))
	if keyword == "" {
		return nil, badRequest(msgInputKeyword)
	}
	if utf8.RuneCountInString(keyword) > maxKeywordRunes {
		return nil, badRequest(msgKeywordTooLong)
	}
	providerName := q.Get("provider")
	if providerName != "" {
		if _, ok := a.providers.Get(providerName); !ok {
			return nil, badRequest(msgUnknownProvider)
		}
	}
	_, offset := pageParams(r)
	// 缓存未命中才会请求音乐平台，只在这时限流；翻页取的是同一份缓存
	results, ok := a.cachedSearch(providerName, keyword)
	if !ok {
		if ok, retryAfter := a.web.guard.bySearch.allow(user.ID, time.Now()); !ok {
			return nil, &rateLimitError{retryAfter}
		}
		select {
		case a.web.guard.search <- struct{}{}:
			defer func() { <-a.web.guard.search }()
		default:
			return nil, &apiError{http.StatusServiceUnavailable, msgSearchBusy}
		}
		var err error
		if results, err = a.searchTracks(r.Context(), providerName, keyword); err != nil {
			a.log.Errorln(err)
			return nil, &apiError{http.StatusBadGateway, msgSearchFailed}
		}
	}
	summaries := results[min(offset, len(results)):min(offset+searchPageSize, len(results))]
	type item struct {
		refDTO
		ProviderName string `json:"provider_name"`
		Title        string `json:"title"`
		Artists      string `json:"artists"`
		Album        string `json:"album"`
		Duration     int    `json:"duration"`
		Cached       bool   `json:"cached"`
	}
	items := make([]item, 0, len(summaries))
	for _, s := range summaries {
		// 子资源（节目等）的缓存键需要归一后才知道，这里不标记
		cached := false
		if s.Sub == "" {
			info, ok := a.store.GetByRef(s.Provider, s.ID)
			cached = ok && info.FileID != ""
		}
		items = append(items, item{
			refDTO:       refDTO{Provider: s.Provider, ID: s.ID, Sub: s.Sub},
			ProviderName: a.providerDisplay(s.Provider),
			Title:        s.Title,
			Artists:      textx.JoinNames(s.Artists),
			Album:        s.Album,
			Duration:     s.DurationMS / 1000,
			Cached:       cached,
		})
	}
	return map[string]any{"items": items, "total": len(results), "page_size": searchPageSize}, nil
}

// apiMusic 把歌曲发到用户与 bot 的私聊：命中缓存时直接发送，否则下载后发送。
// 请求立即返回，进度由私聊里的状态消息展示。
func (a *App) apiMusic(r *http.Request, user webapp.User) (any, error) {
	var req refDTO
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if _, ok := a.providers.Get(req.Provider); !ok || req.ID == "" || len(req.ID) > maxRefBytes || len(req.Sub) > maxRefBytes {
		return nil, badRequest(msgSongNotFound)
	}
	done, ok := a.web.guard.beginMusic(user.ID)
	if !ok {
		return nil, &apiError{http.StatusTooManyRequests, msgMusicBusy}
	}
	if remain := a.cooldown("music", user.ID, searchMusicCooldown); remain > 0 {
		done()
		return nil, &apiError{http.StatusTooManyRequests, msgCooldown(int(math.Ceil(remain.Seconds())))}
	}
	ref := provider.Ref{Provider: req.Provider, ID: req.ID, Sub: req.Sub}
	target := musicTarget{
		origin: requestOrigin{userID: user.ID, userName: user.Username, chatID: user.ID, chatName: user.Username, source: sourceWebApp},
		chatID: user.ID,
	}
	a.goInflight(func(ctx context.Context) {
		defer done()
		if err := a.processMusic(ctx, ref, target, nil); err != nil {
			a.log.Errorln(err)
		}
	})
	return nil, nil
}
