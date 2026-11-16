package bot

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// rateLimiter 是按 key 分桶的令牌桶：每个 key 每秒补充 rate 个令牌，最多积攒 burst 个。
type rateLimiter[K comparable] struct {
	rate  float64
	burst float64

	mu        sync.Mutex
	buckets   map[K]*tokenBucket
	lastSweep time.Time
}

type tokenBucket struct {
	tokens float64
	at     time.Time
}

func newRateLimiter[K comparable](rate float64, burst int) *rateLimiter[K] {
	return &rateLimiter[K]{rate: rate, burst: float64(burst), buckets: make(map[K]*tokenBucket)}
}

// allow 取走一个令牌；令牌不足时返回还需等待的时间。
func (l *rateLimiter[K]) allow(key K, now time.Time) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	b := l.buckets[key]
	if b == nil {
		b = &tokenBucket{tokens: l.burst, at: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.at).Seconds()*l.rate)
	b.at = now
	if b.tokens < 1 {
		return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	}
	b.tokens--
	return true, 0
}

// sweep 定期删除已经补满的桶，让表的大小只取决于近期活跃的 key。
func (l *rateLimiter[K]) sweep(now time.Time) {
	refill := time.Duration(l.burst / l.rate * float64(time.Second))
	if now.Sub(l.lastSweep) < refill {
		return
	}
	l.lastSweep = now
	for key, b := range l.buckets {
		if now.Sub(b.at) >= refill {
			delete(l.buckets, key)
		}
	}
}

// 面板的限流参数。前端正常使用时的请求量：打开页面约 10 个请求，之后以秒级间隔轮询。
const (
	// 按客户端 IP，校验身份之前生效，覆盖静态文件与无效 initData 的请求。
	// 同一出口 IP 下可能有多个用户，额度比单用户宽。
	ipRate, ipBurst = 20, 100
	// 按用户，全部接口共用
	userRate, userBurst = 5, 30
	// 按用户的搜索：每次搜索都会调用音乐平台的接口
	searchRate, searchBurst = 1.0 / 3, 6
	// 面板同时进行的搜索数。搜索与 bot 命令共用插件实例，留出余量给聊天中的请求。
	searchConcurrency = 2
)

type webGuard struct {
	byIP     *rateLimiter[string]
	byUser   *rateLimiter[int64]
	bySearch *rateLimiter[int64]
	search   chan struct{} // 搜索并发位

	musicMu   sync.Mutex
	musicBusy map[int64]bool // 有点歌任务在处理中的用户
}

func newWebGuard() *webGuard {
	return &webGuard{
		byIP:      newRateLimiter[string](ipRate, ipBurst),
		byUser:    newRateLimiter[int64](userRate, userBurst),
		bySearch:  newRateLimiter[int64](searchRate, searchBurst),
		search:    make(chan struct{}, searchConcurrency),
		musicBusy: make(map[int64]bool),
	}
}

// beginMusic 登记用户的点歌任务，同一用户同时只处理一个；返回的函数在任务结束时调用。
func (g *webGuard) beginMusic(userID int64) (done func(), ok bool) {
	g.musicMu.Lock()
	defer g.musicMu.Unlock()
	if g.musicBusy[userID] {
		return nil, false
	}
	g.musicBusy[userID] = true
	return func() {
		g.musicMu.Lock()
		delete(g.musicBusy, userID)
		g.musicMu.Unlock()
	}, true
}

func tooManyRequests(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
	writeError(w, r, http.StatusTooManyRequests, msgTooManyRequests)
}

// clientIP 返回限流用的客户端地址。只有连接来自本机或内网（即经过 cloudflared 或反向代理）
// 时才采信转发头，直连的客户端无法借此伪造地址。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer == nil || !(peer.IsLoopback() || peer.IsPrivate()) {
		return host
	}
	// Cloudflare 会覆盖客户端自带的 CF-Connecting-IP；X-Forwarded-For 取最近一级代理追加的末项
	for _, value := range []string{r.Header.Get("CF-Connecting-IP"), r.Header.Get("X-Real-IP"), lastField(r.Header.Get("X-Forwarded-For"))} {
		if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil {
			return ip.String()
		}
	}
	return host
}

func lastField(list string) string {
	return list[strings.LastIndexByte(list, ',')+1:]
}

// contentSecurityPolicy 只允许加载自身资源与 Telegram 的 SDK 脚本，并限制可嵌入本页面的站点：
// Telegram 网页版用 iframe 打开 Mini App，其他客户端使用 WebView，不受 frame-ancestors 影响。
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' https://telegram.org; " +
	"style-src 'self' 'unsafe-inline'; " + // 组件用内联 style 属性设置尺寸与位移
	"img-src 'self' data:; " + // 登录二维码以 data URL 返回
	"connect-src 'self'; " +
	"object-src 'none'; base-uri 'none'; form-action 'self'; " +
	"frame-ancestors https://web.telegram.org https://*.telegram.org"

// guard 是全部请求的入口：设置安全响应头并按客户端 IP 限流。
func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if ok, retryAfter := a.web.guard.byIP.allow(clientIP(r), time.Now()); !ok {
			tooManyRequests(w, r, retryAfter)
			return
		}
		next.ServeHTTP(w, r)
	})
}
