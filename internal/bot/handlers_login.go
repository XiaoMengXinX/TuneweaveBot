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

func (a *App) handleLogin(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	if !a.loginMu.TryLock() {
		if _, err := a.reply(ctx, m, render.LoginBusy); err != nil {
			a.log.Errorln(err)
		}
		return
	}
	defer a.loginMu.Unlock()

	providerName := strings.TrimSpace(commandArguments(m))
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
	challenge, err := auth.Begin(ctx)
	if err != nil {
		a.log.Errorln(err)
		if _, err := a.reply(ctx, m, render.LoginKeyFailed); err != nil {
			a.log.Errorln(err)
		}
		return
	}
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

	sessCtx, cancelSession := context.WithCancelCause(ctx)
	defer cancelSession(nil)
	a.setLoginCancel(cancelSession)
	defer a.setLoginCancel(nil)
	a.addLoginSurface(m.Chat.ID, photo.ID)

	editCaption := func(text string, markup models.ReplyMarkup) error {
		_, err := a.tg.EditMessageCaption(ctx, &tgb.EditMessageCaptionParams{
			ChatID:      m.Chat.ID,
			MessageID:   photo.ID,
			Caption:     text,
			ParseMode:   models.ParseModeHTML,
			ReplyMarkup: markup,
		})
		return err
	}

	a.pollQRLogin(sessCtx, auth, challenge.Key, editCaption)
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
			if _, err := a.tg.EditMessageCaption(ctx, &tgb.EditMessageCaptionParams{
				ChatID:      msg.Chat.ID,
				MessageID:   msg.ID,
				Caption:     render.LoginSessionOver,
				ParseMode:   models.ParseModeHTML,
				ReplyMarkup: emptyKeyboard(),
			}); err != nil {
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

// loginQRValidity 仅用于倒计时，以服务端过期状态为准
const loginQRValidity = 3 * time.Minute

func (a *App) pollQRLogin(ctx context.Context, auth provider.Authenticator, key string, editCaption func(string, models.ReplyMarkup) error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	countdown := time.NewTicker(10 * time.Second)
	defer countdown.Stop()
	expiresAt := time.Now().Add(loginQRValidity)
	deadline := time.After(5 * time.Minute) // 二维码约 3 分钟过期，此处兜底
	// 803 可能不带昵称，保留 802 的昵称
	var scannedNick string
	var failures int
	for {
		select {
		case <-ctx.Done():
			if errors.Is(context.Cause(ctx), errLoginCancelled) {
				_ = editCaption(render.LoginCancelled, emptyKeyboard())
			}
			return
		case <-deadline:
			_ = editCaption(render.LoginExpired, emptyKeyboard())
			return
		case <-countdown.C:
			remain := int(math.Ceil(time.Until(expiresAt).Seconds()))
			if remain < 0 {
				remain = 0
			}
			if scannedNick == "" {
				_ = editCaption(render.LoginQRCaption(remain), cancelLoginKeyboard())
			}
		case <-ticker.C:
			state, err := auth.Poll(ctx, key)
			if err != nil {
				failures++
				if failures >= 10 {
					a.log.Errorln(err)
					_ = editCaption(render.LoginCheckFailed, emptyKeyboard())
					return
				}
				continue
			}
			failures = 0
			switch state.State {
			case provider.AuthWaiting:
			case provider.AuthScanned:
				if scannedNick == "" {
					scannedNick = state.Nickname
					if err := editCaption(render.LoginScanned(state.Nickname), cancelLoginKeyboard()); err != nil {
						a.log.Errorln(err)
					}
				}
			case provider.AuthSuccess:
				if state.Nickname == "" {
					state.Nickname = scannedNick
				}
				note, err := a.finishLogin(auth, state)
				if err != nil {
					_ = editCaption(render.LoginFailed+render.Escape(note), emptyKeyboard())
					return
				}
				_ = editCaption(render.LoginOK(state.Nickname)+render.Escape(note), emptyKeyboard())
				return
			case provider.AuthExpired:
				_ = editCaption(render.LoginExpired, emptyKeyboard())
				return
			}
		}
	}
}
