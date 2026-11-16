package bot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"gorm.io/gorm/logger"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

// signedInitData 生成以 token 签名的 initData。
func signedInitData(token string, userID int64) string {
	user := fmt.Sprintf(`{"id":%d,"first_name":"U"}`, userID)
	authDate := fmt.Sprint(time.Now().Unix())
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte("auth_date=" + authDate + "\nuser=" + user))
	return url.Values{"auth_date": {authDate}, "user": {user}, "hash": {hex.EncodeToString(mac.Sum(nil))}}.Encode()
}

func TestWebAppAuth(t *testing.T) {
	a := newTestApp(nil) // 管理员为 42
	st, err := store.Open(filepath.Join(t.TempDir(), "cache.db"), logger.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a.store = st
	a.cfg.Log.Path = t.TempDir()
	a.web.guard = newWebGuard()
	srv := httptest.NewServer(a.guard(a.webHandler()))
	t.Cleanup(srv.Close)

	get := func(path, initData string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if initData != "" {
			req.Header.Set("Authorization", "tma "+initData)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(res.Body).Decode(&body)
		return res.StatusCode, body
	}

	admin, user := signedInitData(a.cfg.Bot.Token, 42), signedInitData(a.cfg.Bot.Token, 7)
	for _, tt := range []struct {
		name, path, initData string
		want                 int
	}{
		{"缺少 initData", "/api/me", "", http.StatusUnauthorized},
		{"签名不符", "/api/me", signedInitData("2:other", 42), http.StatusUnauthorized},
		{"普通用户访问用户接口", "/api/overview", user, http.StatusOK},
		{"普通用户访问管理接口", "/api/admin/logs", user, http.StatusForbidden},
		{"管理员访问管理接口", "/api/admin/logs", admin, http.StatusOK},
	} {
		if got, _ := get(tt.path, tt.initData); got != tt.want {
			t.Errorf("%s: status = %d, want %d", tt.name, got, tt.want)
		}
	}
	// 没有数据的列表要编码成 []，前端按数组处理
	_, stats := get("/api/admin/stats?days=7", admin)
	for _, key := range []string{"requests_by_source", "top_songs", "top_users", "top_chats", "requests_by_provider", "cache_by_provider"} {
		if list, ok := stats[key].([]any); !ok || list == nil {
			t.Errorf("空库的 %s 应为空数组, got %v", key, stats[key])
		}
	}
	if _, me := get("/api/me", user); me["admin"] != false {
		t.Errorf("普通用户的 admin 应为 false: %v", me)
	}
	if _, me := get("/api/me", admin); me["admin"] != true {
		t.Errorf("管理员的 admin 应为 true: %v", me)
	}
}

func TestRateLimiter(t *testing.T) {
	l := newRateLimiter[string](1, 3) // 每秒 1 个，最多积攒 3 个
	now := time.Unix(1_800_000_000, 0)
	for i := range 3 {
		if ok, _ := l.allow("a", now); !ok {
			t.Fatalf("第 %d 次应放行", i+1)
		}
	}
	ok, retryAfter := l.allow("a", now)
	if ok || retryAfter != time.Second {
		t.Fatalf("令牌用尽后应拒绝并提示 1s 后重试, got %v %v", ok, retryAfter)
	}
	if ok, _ := l.allow("b", now); !ok {
		t.Error("不同 key 互不影响")
	}
	if ok, _ := l.allow("a", now.Add(time.Second)); !ok {
		t.Error("补充令牌后应放行")
	}
	// 长时间不活跃的 key 被清理
	l.allow("c", now.Add(time.Hour))
	if len(l.buckets) != 1 {
		t.Errorf("清理后应只剩活跃的 key, got %d", len(l.buckets))
	}
}

func TestClientIP(t *testing.T) {
	for _, tt := range []struct {
		name, remote string
		header       http.Header
		want         string
	}{
		{"经 cloudflared 转发", "127.0.0.1:5000", http.Header{"Cf-Connecting-Ip": {"203.0.113.7"}}, "203.0.113.7"},
		{"反向代理追加的 X-Forwarded-For 末项", "10.0.0.2:5000", http.Header{"X-Forwarded-For": {"1.1.1.1, 203.0.113.8"}}, "203.0.113.8"},
		{"公网直连不采信转发头", "198.51.100.4:5000", http.Header{"Cf-Connecting-Ip": {"203.0.113.7"}}, "198.51.100.4"},
		{"本机直连且没有转发头", "127.0.0.1:5000", http.Header{}, "127.0.0.1"},
	} {
		r := &http.Request{RemoteAddr: tt.remote, Header: tt.header}
		if got := clientIP(r); got != tt.want {
			t.Errorf("%s: clientIP = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestWebAppGuard(t *testing.T) {
	a := newTestApp(nil)
	a.web.guard = newWebGuard()
	srv := httptest.NewServer(a.guard(a.webHandler()))
	t.Cleanup(srv.Close)

	// 无效请求也计入按 IP 的限流，超出突发额度后返回 429。
	// 补充速率调到极低，结果不受测试机负载影响。
	const burst = 3
	a.web.guard.byIP = newRateLimiter[string](0.001, burst)
	var last *http.Response
	for range burst + 1 {
		res, err := http.Get(srv.URL + "/api/me")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		last = res
	}
	if last.StatusCode != http.StatusTooManyRequests || last.Header.Get("Retry-After") == "" {
		t.Errorf("超出额度后 status = %d, Retry-After = %q", last.StatusCode, last.Header.Get("Retry-After"))
	}
	if last.Header.Get("Content-Security-Policy") == "" || last.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("响应缺少安全头")
	}

	// 同一用户同时只处理一个点歌任务
	done, ok := a.web.guard.beginMusic(7)
	if _, again := a.web.guard.beginMusic(7); !ok || again {
		t.Error("任务未结束时不应再次登记")
	}
	done()
	if _, ok := a.web.guard.beginMusic(7); !ok {
		t.Error("任务结束后应可再次登记")
	}
}

func TestReadLogs(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("2026-11-16.log", "2026/11/16 09:00:00 [INFO] 昨天 (app.go:1)\n")
	write("2026-11-17.log", "2026/11/17 10:00:00 [INFO] 启动 (app.go:1)\n"+
		"2026/11/17 10:00:01 [ERROR] panic: boom\n堆栈第一行 (middleware.go:19)\n")
	write("notes.txt", "不是日志")

	days, err := logDays(dir)
	if err != nil || !slices.Equal(days, []string{"2026-11-17", "2026-11-16"}) {
		t.Fatalf("logDays = %v, %v", days, err)
	}

	// 只读指定日期的文件，新的在前，跨行消息并入同一条
	entries, err := readLogs(dir, "2026-11-17", "")
	if err != nil || len(entries) != 2 {
		t.Fatalf("readLogs = %+v, %v", entries, err)
	}
	if got := entries[0]; got.Level != "ERROR" || got.Message != "panic: boom\n堆栈第一行" || got.Caller != "middleware.go:19" {
		t.Errorf("跨行日志解析错误: %+v", got)
	}
	if entries, _ := readLogs(dir, "2026-11-17", "INFO"); len(entries) != 1 || entries[0].Message != "启动" {
		t.Errorf("按级别筛选错误: %+v", entries)
	}
	if entries, err := readLogs(dir, "2026-11-15", ""); err != nil || len(entries) != 0 {
		t.Errorf("没有日志的日期应返回空: %+v, %v", entries, err)
	}
	for _, day := range []string{"../config", "2026-11-17/../../x", "2026-1-7", ""} {
		if logDay.MatchString(day) {
			t.Errorf("%q 不应被接受为日期", day)
		}
	}
}

func TestWebAppErrorLanguage(t *testing.T) {
	a := newTestApp(nil)
	a.web.guard = newWebGuard()
	srv := httptest.NewServer(a.guard(a.webHandler()))
	t.Cleanup(srv.Close)
	for lang, want := range map[string]string{"": msgNotInTelegram.zh, "zh": msgNotInTelegram.zh, "en": msgNotInTelegram.en} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/me", nil)
		req.Header.Set("X-Language", lang)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]string
		_ = json.NewDecoder(res.Body).Decode(&body)
		res.Body.Close()
		if body["error"] != want {
			t.Errorf("X-Language %q: error = %q, want %q", lang, body["error"], want)
		}
	}
}
