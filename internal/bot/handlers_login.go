package bot

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/skip2/go-qrcode"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
)

var errLoginCancelled = errors.New("login cancelled")

// loginQRValidity 仅用于倒计时，以服务端过期状态为准
const loginQRValidity = 3 * time.Minute

// tokenLoginBudget 是 token 登录等待用户回复的超时
const tokenLoginBudget = 3 * time.Minute

func (a *App) handleLogin(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	if !a.loginMu.TryLock() {
		if _, err := a.reply(ctx, m, render.LoginBusy); err != nil {
			a.log.Errorln(err)
		}
		return
	}
	defer a.loginMu.Unlock()

	providerName, mode := parseLoginArgs(commandArguments(m))
	if providerName == "" {
		providerName = a.providers.DefaultName()
	} else if p, ok := a.providers.Lookup(providerName); ok {
		providerName = p.Name()
	}
	auth, ok := a.auth.Get(providerName)
	if !ok {
		if _, err := a.reply(ctx, m, render.LoginUnsupported); err != nil {
			a.log.Errorln(err)
		}
		return
	}

	challenge, err := auth.Begin(ctx, mode)
	if err != nil {
		a.log.Errorln(err)
		if _, err := a.reply(ctx, m, render.LoginKeyFailed); err != nil {
			a.log.Errorln(err)
		}
		return
	}

	sessCtx, cancelSession := context.WithCancelCause(ctx)
	defer cancelSession(nil)
	a.setLoginCancel(cancelSession)
	defer a.setLoginCancel(nil)

	switch challenge.Type {
	case "token":
		a.runTokenLogin(sessCtx, auth, m, challenge)
	default:
		a.runQRLogin(sessCtx, auth, m, challenge)
	}
}

// parseLoginArgs 解析 /login 参数：provider 名与可选的登录方式（token）。
func parseLoginArgs(args string) (providerName, mode string) {
	fields := strings.Fields(args)
	for _, f := range fields {
		if f == "token" {
			mode = "token"
			continue
		}
		if providerName == "" {
			providerName = f
		}
	}
	return providerName, mode
}

// runQRLogin 发送二维码并轮询登录结果。
func (a *App) runQRLogin(ctx context.Context, auth provider.Authenticator, m *models.Message, challenge *provider.AuthChallenge) {
	png, err := qrcode.Encode(challenge.Content, qrcode.High, 512)
	if err != nil {
		a.log.Errorln(err)
		if _, err := a.reply(ctx, m, render.LoginKeyFailed); err != nil {
			a.log.Errorln(err)
		}
		return
	}
	photo, err := a.tg.SendPhoto(ctx, &tgb.SendPhotoParams{
		ChatID:          m.Chat.ID,
		Photo:           &models.InputFileUpload{Filename: "login-qr.png", Data: bytes.NewReader(png)},
		Caption:         render.LoginQRCaption(int(loginQRValidity / time.Second)),
		ParseMode:       models.ParseModeHTML,
		ReplyParameters: &models.ReplyParameters{MessageID: m.ID},
		ReplyMarkup:     cancelLoginKeyboard(),
	})
	if err != nil {
		a.log.Errorln(err)
		return
	}
	a.addLoginSurface(m.Chat.ID, photo.ID)

	editCaption := func(text string, markup models.ReplyMarkup) error {
		// 终态提示即便会话已被取消/过期也要送达，用剥离取消信号的 ctx
		editCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, err := a.tg.EditMessageCaption(editCtx, &tgb.EditMessageCaptionParams{
			ChatID:      m.Chat.ID,
			MessageID:   photo.ID,
			Caption:     text,
			ParseMode:   models.ParseModeHTML,
			ReplyMarkup: markup,
		})
		return err
	}
	a.pollQRLogin(ctx, auth, challenge.Key, func(ev loginEvent) {
		text, markup := ev.text(), emptyKeyboard()
		switch ev.state {
		case loginWaiting:
			text, markup = render.LoginQRCaption(ev.remain), cancelLoginKeyboard()
		case loginScanned:
			markup = cancelLoginKeyboard()
		}
		if err := editCaption(text, markup); err != nil && ev.state == loginScanned {
			a.log.Errorln(err)
		}
	})
}

// runTokenLogin 提示用户回复 token，捕获后提交校验。
func (a *App) runTokenLogin(ctx context.Context, auth provider.Authenticator, m *models.Message, challenge *provider.AuthChallenge) {
	prompt, err := a.tg.SendMessage(ctx, &tgb.SendMessageParams{
		ChatID:          m.Chat.ID,
		Text:            challenge.Content,
		ParseMode:       models.ParseModeHTML,
		ReplyParameters: &models.ReplyParameters{MessageID: m.ID},
		ReplyMarkup:     cancelLoginKeyboard(),
	})
	if err != nil {
		a.log.Errorln(err)
		return
	}
	a.addLoginSurface(m.Chat.ID, prompt.ID)

	token, ok := a.waitToken(ctx, m.Chat.ID, prompt.ID)
	edit := func(text string) {
		// 终态提示即便会话已被取消/过期也要送达，用剥离取消信号的 ctx
		editCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := a.tg.EditMessageText(editCtx, &tgb.EditMessageTextParams{
			ChatID:      m.Chat.ID,
			MessageID:   prompt.ID,
			Text:        text,
			ParseMode:   models.ParseModeHTML,
			ReplyMarkup: emptyKeyboard(),
		}); err != nil {
			a.log.Errorln(err)
		}
	}
	if !ok {
		edit(loginAbortEvent(ctx).text())
		return
	}

	edit(a.submitToken(ctx, auth, challenge.Key, token).text())
}

// submitToken 提交 token 并在校验通过后应用凭据，返回终态。
func (a *App) submitToken(ctx context.Context, auth provider.Authenticator, key, token string) loginEvent {
	state, err := auth.Submit(ctx, key, token)
	if err != nil {
		a.log.Errorln(err)
		return loginEvent{state: loginCheckFailed}
	}
	if state.State != provider.AuthSuccess {
		return loginEvent{state: loginFailed}
	}
	note, err := a.finishLogin(auth, state)
	if err != nil {
		return loginEvent{state: loginFailed, note: note}
	}
	return loginEvent{state: loginSuccess, nickname: state.Nickname, note: note}
}

// 登录会话的状态；waiting 与 scanned 之外都是终态。
const (
	loginWaiting     = "waiting"
	loginScanned     = "scanned"
	loginSuccess     = "success"
	loginFailed      = "failed"
	loginExpired     = "expired"
	loginCancelled   = "cancelled"
	loginReloading   = "reloading"
	loginCheckFailed = "check_failed"
)

// loginEvent 是登录会话的进展，由 Telegram 消息或看板各自展示。
type loginEvent struct {
	state    string
	nickname string
	note     string // 配置写回失败等附加说明
	remain   int    // waiting：二维码剩余有效秒数
}

func (e loginEvent) terminal() bool { return e.state != loginWaiting && e.state != loginScanned }

// text 是事件在 Telegram 消息中的文案。
func (e loginEvent) text() string {
	switch e.state {
	case loginScanned:
		return render.LoginScanned(e.nickname)
	case loginSuccess:
		return render.LoginOK(e.nickname) + render.Escape(e.note)
	case loginFailed:
		return render.LoginFailed + render.Escape(e.note)
	case loginExpired:
		return render.LoginExpired
	case loginCancelled:
		return render.LoginCancelled
	case loginReloading:
		return render.LoginReloading
	case loginCheckFailed:
		return render.LoginCheckFailed
	}
	return ""
}

// loginAbortEvent 把会话 ctx 的结束原因映射为终态；非取消、非重载的结束视为过期。
func loginAbortEvent(ctx context.Context) loginEvent {
	switch cause := context.Cause(ctx); {
	case errors.Is(cause, errLoginCancelled):
		return loginEvent{state: loginCancelled}
	case errors.Is(cause, ErrReload):
		return loginEvent{state: loginReloading}
	}
	return loginEvent{state: loginExpired}
}

func cancelLoginKeyboard() models.InlineKeyboardMarkup {
	return models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: render.CancelLogin, CallbackData: "cancellogin"},
		}},
	}
}

// emptyKeyboard 编辑消息时用于移除 inline 按钮
func emptyKeyboard() models.InlineKeyboardMarkup {
	return models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}
}

// surfaceKey 标识一条带登录按钮的消息。
type surfaceKey struct {
	chatID    int64
	messageID int
}

// loginMu 由会话持有，取消函数须用独立锁保护
func (a *App) setLoginCancel(cancel context.CancelCauseFunc) {
	a.loginCancelMu.Lock()
	defer a.loginCancelMu.Unlock()
	a.loginCancel = cancel
	// 新会话开始/结束时重置已登记的按钮消息
	a.loginSurfaces = map[surfaceKey]bool{}
}

// addLoginSurface 登记一条属于当前会话的登录按钮消息。
func (a *App) addLoginSurface(chatID int64, messageID int) {
	a.loginCancelMu.Lock()
	defer a.loginCancelMu.Unlock()
	if a.loginSurfaces != nil {
		a.loginSurfaces[surfaceKey{chatID, messageID}] = true
	}
}

// cancelLoginSession 仅当点击的消息属于当前会话时才取消，返回是否取消成功。
func (a *App) cancelLoginSession(chatID int64, messageID int) bool {
	a.loginCancelMu.Lock()
	defer a.loginCancelMu.Unlock()
	if a.loginCancel == nil || !a.loginSurfaces[surfaceKey{chatID, messageID}] {
		return false
	}
	a.loginCancel(errLoginCancelled)
	return true
}

func (a *App) handleCallbackCancelLogin(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	query := update.CallbackQuery
	msg := query.Message.Message
	var chatID int64
	var messageID int
	if msg != nil {
		chatID, messageID = msg.Chat.ID, msg.ID
	}
	if !a.cancelLoginSession(chatID, messageID) {
		// 不属于当前会话（旧消息/会话已结束）：只移除按钮，不影响进行中的会话
		if msg != nil {
			// 二维码登录是带 caption 的图片，token 登录是纯文本，需用对应的编辑接口
			var err error
			if len(msg.Photo) > 0 {
				_, err = a.tg.EditMessageCaption(ctx, &tgb.EditMessageCaptionParams{
					ChatID:      msg.Chat.ID,
					MessageID:   msg.ID,
					Caption:     render.LoginSessionOver,
					ParseMode:   models.ParseModeHTML,
					ReplyMarkup: emptyKeyboard(),
				})
			} else {
				_, err = a.tg.EditMessageText(ctx, &tgb.EditMessageTextParams{
					ChatID:      msg.Chat.ID,
					MessageID:   msg.ID,
					Text:        render.LoginSessionOver,
					ParseMode:   models.ParseModeHTML,
					ReplyMarkup: emptyKeyboard(),
				})
			}
			if err != nil {
				a.log.Errorln(err)
			}
		}
		if _, err := a.tg.AnswerCallbackQuery(ctx, &tgb.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            render.LoginNoSession,
		}); err != nil {
			a.log.Errorln(err)
		}
		return
	}
	if _, err := a.tg.AnswerCallbackQuery(ctx, &tgb.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
		Text:            render.LoginCancelled,
	}); err != nil {
		a.log.Errorln(err)
	}
}

// pollQRLogin 轮询扫码结果并通过 report 上报进展，直到会话进入终态。
func (a *App) pollQRLogin(ctx context.Context, auth provider.Authenticator, key string, report func(loginEvent)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	countdown := time.NewTicker(10 * time.Second)
	defer countdown.Stop()
	expiresAt := time.Now().Add(loginQRValidity)
	deadline := time.After(5 * time.Minute) // 二维码约 3 分钟过期，此处兜底
	// 803 可能不带昵称，保留 802 的昵称
	var scannedNick string
	var scanned bool
	var failures int
	for {
		select {
		case <-ctx.Done():
			switch cause := context.Cause(ctx); {
			case errors.Is(cause, errLoginCancelled):
				report(loginEvent{state: loginCancelled})
			case errors.Is(cause, ErrReload):
				report(loginEvent{state: loginReloading})
			}
			return
		case <-deadline:
			report(loginEvent{state: loginExpired})
			return
		case <-countdown.C:
			if !scanned {
				remain := max(0, int(math.Ceil(time.Until(expiresAt).Seconds())))
				report(loginEvent{state: loginWaiting, remain: remain})
			}
		case <-ticker.C:
			state, err := auth.Poll(ctx, key)
			if err != nil {
				failures++
				if failures >= 10 {
					a.log.Errorln(err)
					report(loginEvent{state: loginCheckFailed})
					return
				}
				continue
			}
			failures = 0
			switch state.State {
			case provider.AuthWaiting:
			case provider.AuthScanned:
				if !scanned {
					scanned, scannedNick = true, state.Nickname
					report(loginEvent{state: loginScanned, nickname: state.Nickname})
				}
			case provider.AuthSuccess:
				if state.Nickname == "" {
					state.Nickname = scannedNick
				}
				note, err := a.finishLogin(auth, state)
				if err != nil {
					report(loginEvent{state: loginFailed, note: note})
					return
				}
				report(loginEvent{state: loginSuccess, nickname: state.Nickname, note: note})
				return
			case provider.AuthExpired:
				report(loginEvent{state: loginExpired})
				return
			}
		}
	}
}
