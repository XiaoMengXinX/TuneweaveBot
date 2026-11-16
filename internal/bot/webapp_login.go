package bot

import (
	"context"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/webapp"
)

// webLogin 是从看板发起的登录会话，结束后保留终态供页面读取。字段由 webState.loginMu 保护。
type webLogin struct {
	id        string
	provider  string
	kind      string // qr | link | token
	key       string // 轮询或提交 key
	content   string // link：登录链接；token：提示文案
	qr        string // qr：二维码 PNG 的 data URL
	expiresAt time.Time
	event     loginEvent
	token     chan string
	cancel    context.CancelCauseFunc
}

func (a *App) setWebLoginEvent(session *webLogin, ev loginEvent) {
	a.web.loginMu.Lock()
	defer a.web.loginMu.Unlock()
	session.event = ev
}

func (a *App) apiLoginState(r *http.Request, user webapp.User) (any, error) {
	type authDTO struct {
		Name      string `json:"name"`
		Display   string `json:"display"`
		Available bool   `json:"available"`
	}
	names := a.auth.Names()
	providers := make([]authDTO, 0, len(names))
	for _, name := range names {
		providers = append(providers, authDTO{Name: name, Display: a.providerDisplay(name), Available: a.auth.Available(name)})
	}

	var session map[string]any
	active := false
	a.web.loginMu.Lock()
	if s := a.web.login; s != nil {
		active = !s.event.terminal()
		session = map[string]any{
			"id":         s.id,
			"provider":   s.provider,
			"type":       s.kind,
			"content":    s.content,
			"qr":         s.qr,
			"expires_at": s.expiresAt.Unix(),
			"state":      s.event.state,
			"nickname":   s.event.nickname,
			"note":       s.event.note,
		}
	}
	a.web.loginMu.Unlock()

	// 没有进行中的看板会话却拿不到登录锁，说明 /login 命令的会话正在进行
	busy := false
	if !active {
		if a.loginMu.TryLock() {
			a.loginMu.Unlock()
		} else {
			busy = true
		}
	}
	return map[string]any{"providers": providers, "session": session, "busy": busy}, nil
}

func (a *App) apiLoginBegin(r *http.Request, user webapp.User) (any, error) {
	var req struct {
		Provider string `json:"provider"`
		Mode     string `json:"mode"`
	}
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	auth, ok := a.auth.Get(req.Provider)
	if !ok {
		return nil, badRequest(render.LoginUnsupported)
	}
	if !a.loginMu.TryLock() {
		return nil, conflict(render.LoginBusy)
	}
	session, err := a.beginWebLogin(r.Context(), auth, req.Mode)
	if err != nil {
		a.loginMu.Unlock()
		a.log.Errorln(err)
		return nil, &apiError{http.StatusBadGateway, render.LoginKeyFailed}
	}

	sessCtx, cancel := context.WithCancelCause(a.workCtx)
	session.cancel = cancel
	a.setLoginCancel(cancel)
	a.web.loginMu.Lock()
	a.web.login = session
	a.web.loginMu.Unlock()
	a.goInflight(func(context.Context) {
		defer a.loginMu.Unlock()
		defer a.setLoginCancel(nil)
		defer cancel(nil)
		a.runWebLogin(sessCtx, auth, session)
	})
	return a.apiLoginState(r, user)
}

func (a *App) beginWebLogin(ctx context.Context, auth provider.Authenticator, mode string) (*webLogin, error) {
	challenge, err := auth.Begin(ctx, mode)
	if err != nil {
		return nil, err
	}
	session := &webLogin{
		id:       newUpdateToken(),
		provider: auth.Name(),
		kind:     challenge.Type,
		content:  challenge.Content,
		token:    make(chan string, 1),
		event:    loginEvent{state: loginWaiting},
	}
	switch challenge.Type {
	case "token":
		session.expiresAt = time.Now().Add(tokenLoginBudget)
	case "link":
		session.expiresAt = time.Now().Add(loginQRValidity)
	default:
		png, err := qrcode.Encode(challenge.Content, qrcode.Medium, 512)
		if err != nil {
			return nil, err
		}
		session.kind = "qr"
		session.content = ""
		session.qr = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
		session.expiresAt = time.Now().Add(loginQRValidity)
	}
	session.key = challenge.Key
	return session, nil
}

func (a *App) runWebLogin(ctx context.Context, auth provider.Authenticator, session *webLogin) {
	report := func(ev loginEvent) { a.setWebLoginEvent(session, ev) }
	if session.kind != "token" {
		a.pollQRLogin(ctx, auth, session.key, report)
		return
	}
	select {
	case token := <-session.token:
		report(a.submitToken(ctx, auth, session.key, token))
	case <-time.After(time.Until(session.expiresAt)):
		report(loginEvent{state: loginExpired})
	case <-ctx.Done():
		report(loginAbortEvent(ctx))
	}
}

// activeWebLogin 返回进行中的看板登录会话。
func (a *App) activeWebLogin() (*webLogin, bool) {
	a.web.loginMu.Lock()
	defer a.web.loginMu.Unlock()
	s := a.web.login
	return s, s != nil && !s.event.terminal()
}

func (a *App) apiLoginToken(r *http.Request, user webapp.User) (any, error) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if req.Token == "" {
		return nil, badRequest("请输入 token")
	}
	session, ok := a.activeWebLogin()
	if !ok || session.kind != "token" {
		return nil, conflict(render.LoginNoSessionWeb)
	}
	select {
	case session.token <- req.Token:
	default:
		return nil, conflict("token 已提交，正在校验")
	}
	return nil, nil
}

func (a *App) apiLoginCancel(r *http.Request, user webapp.User) (any, error) {
	session, ok := a.activeWebLogin()
	if !ok {
		return nil, conflict(render.LoginNoSessionWeb)
	}
	session.cancel(errLoginCancelled)
	return nil, nil
}
