package bot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/sirupsen/logrus"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/auth"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/config"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
)

func newTestBot(t *testing.T) (*tgb.Bot, *requestRecorder) {
	t.Helper()
	rec := &requestRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(srv.Close)

	tg, err := tgb.New("1:test", tgb.WithServerURL(srv.URL), tgb.WithSkipGetMe(), tgb.WithNotAsyncHandlers())
	if err != nil {
		t.Fatal(err)
	}
	return tg, rec
}

type requestRecorder struct {
	mu     sync.Mutex
	paths  []string
	onPath func(path string)
}

func (r *requestRecorder) add(path string) {
	r.mu.Lock()
	r.paths = append(r.paths, path)
	onPath := r.onPath
	r.mu.Unlock()
	if onPath != nil {
		onPath(path)
	}
}

func (r *requestRecorder) has(prefix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.paths {
		if strings.Contains(p, prefix) {
			return true
		}
	}
	return false
}

func newTestApp(tg *tgb.Bot) *App {
	cfg := config.Default()
	cfg.Bot.Admin = []int64{42}
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	registry, err := provider.NewRegistry(fakeProviderName, newFakeProvider())
	if err != nil {
		panic(err)
	}
	return &App{
		cfg:       cfg,
		tg:        tg,
		me:        &models.User{ID: 1, Username: "testbot"},
		usernames: []string{"testbot"},
		log:       log,
		providers: registry,
		auth:      auth.New(fakeAuthenticator{name: fakeProviderName}),
	}
}

func setTestProvider(t *testing.T, a *App, p *fakeProvider) {
	t.Helper()
	registry, err := provider.NewRegistry(p.Name(), p)
	if err != nil {
		t.Fatal(err)
	}
	a.providers = registry
	a.auth = auth.New(fakeAuthenticator{name: p.Name()})
}

func TestCancelLoginCallbackDispatch(t *testing.T) {
	tg, rec := newTestBot(t)
	a := newTestApp(tg)
	tg.RegisterHandler(tgb.HandlerTypeCallbackQueryData, "cancellogin", tgb.MatchTypeExact, a.handleCallbackCancelLogin, a.adminOnly)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tg.ProcessUpdate(ctx, &models.Update{
		CallbackQuery: &models.CallbackQuery{
			ID:   "cb1",
			From: models.User{ID: 42},
			Data: "cancellogin",
			Message: models.MaybeInaccessibleMessage{Message: &models.Message{
				ID:   1,
				Chat: models.Chat{ID: 42, Type: models.ChatTypePrivate},
			}},
		},
	})

	deadline := time.Now().Add(3 * time.Second)
	for !rec.has("answerCallbackQuery") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !rec.has("answerCallbackQuery") {
		t.Fatalf("点击取消登录后未应答回调, 请求列表: %v", rec.paths)
	}

	sessCtx, sessCancel := context.WithCancelCause(context.Background())
	defer sessCancel(nil)
	a.setLoginCancel(sessCancel)
	// 只有属于当前会话的按钮消息才能取消会话
	a.addLoginSurface(42, 7)
	tg.ProcessUpdate(ctx, &models.Update{
		CallbackQuery: &models.CallbackQuery{
			ID:   "cb2",
			From: models.User{ID: 42},
			Data: "cancellogin",
			Message: models.MaybeInaccessibleMessage{Message: &models.Message{
				ID:   7,
				Chat: models.Chat{ID: 42, Type: models.ChatTypePrivate},
			}},
		},
	})
	deadline = time.Now().Add(3 * time.Second)
	for context.Cause(sessCtx) == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if context.Cause(sessCtx) != errLoginCancelled {
		t.Fatal("取消登录回调没有取消会话")
	}

	// 不属于当前会话的旧按钮不得取消新会话
	sessCtx2, sessCancel2 := context.WithCancelCause(context.Background())
	defer sessCancel2(nil)
	a.setLoginCancel(sessCancel2)
	a.addLoginSurface(42, 7)
	tg.ProcessUpdate(ctx, &models.Update{
		CallbackQuery: &models.CallbackQuery{
			ID:   "cb3",
			From: models.User{ID: 42},
			Data: "cancellogin",
			Message: models.MaybeInaccessibleMessage{Message: &models.Message{
				ID:   999, // 非本会话登记的消息
				Chat: models.Chat{ID: 42, Type: models.ChatTypePrivate},
			}},
		},
	})
	time.Sleep(200 * time.Millisecond)
	if context.Cause(sessCtx2) != nil {
		t.Fatal("旧按钮不应取消当前会话")
	}
}

func TestCancelLoginCallbackNonAdmin(t *testing.T) {
	tg, rec := newTestBot(t)
	a := newTestApp(tg)
	tg.RegisterHandler(tgb.HandlerTypeCallbackQueryData, "cancellogin", tgb.MatchTypeExact, a.handleCallbackCancelLogin, a.adminOnly)

	done := make(chan struct{})
	rec.onPath = func(path string) {
		if strings.Contains(path, "answerCallbackQuery") {
			close(done)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	tg.ProcessUpdate(ctx, &models.Update{
		CallbackQuery: &models.CallbackQuery{
			ID:   "cb3",
			From: models.User{ID: 999},
			Data: "cancellogin",
		},
	})
	select {
	case <-done:
		t.Fatal("非管理员不应触发回调应答")
	case <-ctx.Done():
	}
}

func TestCommandArguments(t *testing.T) {
	tests := []struct {
		text     string
		length   int
		expected string
	}{
		{"/music 12345", 6, "12345"},
		{"/music", 6, ""},
		{"/music  123", 6, " 123"}, // 兼容旧版：仅跳过一个空格
	}
	for _, tt := range tests {
		m := &models.Message{Text: tt.text, Entities: []models.MessageEntity{
			{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: tt.length},
		}}
		if got := commandArguments(m); got != tt.expected {
			t.Errorf("commandArguments(%q) = %q, want %q", tt.text, got, tt.expected)
		}
	}
}

func TestMatchCommand(t *testing.T) {
	mk := func(text string, offset, length int) *models.Update {
		return &models.Update{Message: &models.Message{
			Text:     text,
			Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: offset, Length: length}},
		}}
	}
	match := matchCommand("music", []string{"Tuneweave_app", "Music163bot"})
	tests := []struct {
		u    *models.Update
		want bool
	}{
		{mk("/music", 0, 6), true},
		{mk("/music@Tuneweave_app 1", 0, 20), true},
		{mk("/music@tuneweave_app", 0, 20), true}, // 用户名不区分大小写
		{mk("/music@Music163bot 1", 0, 18), true}, // 通过 Fragment 获得的其他用户名
		{mk("/music@otherbot", 0, 15), false},
		{mk("/musicx", 0, 7), false},
		{mk("hi /music", 3, 6), false},
	}
	for i, tt := range tests {
		if got := match(tt.u); got != tt.want {
			t.Errorf("case %d: match = %v, want %v", i, got, tt.want)
		}
	}
}

func TestBotUsernames(t *testing.T) {
	for _, c := range []struct {
		name string
		resp string
		want []string
	}{
		{"active usernames", `{"ok":true,"result":{"id":1,"type":"private","username":"Tuneweave_app","active_usernames":["Tuneweave_app","Music163bot"]}}`, []string{"Tuneweave_app", "Music163bot"}},
		{"getChat failed", `{"ok":false,"error_code":400,"description":"Bad Request"}`, []string{"Tuneweave_app"}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(c.resp))
		}))
		tg, err := tgb.New("1:test", tgb.WithServerURL(srv.URL), tgb.WithSkipGetMe())
		if err != nil {
			t.Fatal(err)
		}
		got := botUsernames(context.Background(), tg, &models.User{ID: 1, Username: "Tuneweave_app"})
		srv.Close()
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: usernames = %v, want %v", c.name, got, c.want)
		}
	}
}
