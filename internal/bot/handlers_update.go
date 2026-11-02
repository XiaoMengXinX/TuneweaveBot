package bot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/robfig/cron/v3"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/plugin"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
)

const (
	updateCallbackPrefix = "pluginupdate "
	updateOfferTTL       = 24 * time.Hour
	updateOfferValidFor  = "24 小时"
	updateCheckTimeout   = 2 * time.Minute
	updateApplyTimeout   = 10 * time.Minute
)

// updateOffer 是一个"立即更新"按钮对应的待更新插件，过期或使用后失效。
type updateOffer struct {
	names   []string
	expires time.Time
}

// watchPluginUpdates 按 [plugins] update_cron 定时检查插件更新，有更新时通知管理员。
func (a *App) watchPluginUpdates(ctx context.Context) {
	if a.cfg.Plugins.UpdateCron == "" || a.plugins == nil || len(a.cfg.Bot.Admin) == 0 {
		return
	}
	sched, err := cron.ParseStandard(a.cfg.Plugins.UpdateCron)
	if err != nil {
		a.log.Errorf("update_cron 不合法: %v", err)
		return
	}
	for {
		timer := time.NewTimer(time.Until(sched.Next(time.Now())))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		a.notifyPluginUpdates(ctx)
	}
}

func (a *App) notifyPluginUpdates(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()
	updates, _, err := a.plugins.CheckUpdates(checkCtx)
	if err != nil {
		a.log.Warnf("检查插件更新失败: %v", err)
	}
	if len(updates) == 0 {
		return
	}
	text, markup := a.offerUpdates(updates, err)
	for _, adminID := range a.cfg.Bot.Admin {
		if _, err := a.tg.SendMessage(ctx, &tgb.SendMessageParams{
			ChatID:      adminID,
			Text:        text,
			ParseMode:   models.ParseModeHTML,
			ReplyMarkup: markup,
		}); err != nil {
			a.log.Warnf("向管理员 %d 发送插件更新通知失败: %v", adminID, err)
		}
	}
}

func (a *App) handleCheckUpdate(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	status, err := a.reply(ctx, m, render.CheckingUpdates)
	if err != nil {
		a.log.Errorln(err)
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()
	updates, checked, err := a.plugins.CheckUpdates(checkCtx)

	var text string
	var markup models.ReplyMarkup
	switch {
	case checked == 0:
		text = render.NoUpdatablePlugins
	case len(updates) == 0 && err != nil:
		text = render.UpdateCheckFailed(err.Error())
	case len(updates) == 0:
		text = render.NoPluginUpdates
	default:
		text, markup = a.offerUpdates(updates, err)
	}
	if err := a.editMessage(ctx, m.Chat.ID, status.ID, text, markup); err != nil {
		a.log.Errorln(err)
	}
}

// offerUpdates 登记一个带时效的更新按钮，返回通知文本与按钮。
func (a *App) offerUpdates(updates []plugin.Update, checkErr error) (string, models.InlineKeyboardMarkup) {
	names := make([]string, 0, len(updates))
	lines := make([]string, 0, len(updates))
	for _, u := range updates {
		names = append(names, u.Name)
		lines = append(lines, render.PluginUpdateLine(u.DisplayName, u.Name,
			render.PluginVersion(u.Current.Version, u.Current.BuildTime),
			render.PluginVersion(u.Latest.Version, u.Latest.BuildTime)))
	}
	token := newUpdateToken()
	now := time.Now()
	a.updateMu.Lock()
	for t, o := range a.updateOffers {
		if now.After(o.expires) {
			delete(a.updateOffers, t)
		}
	}
	a.updateOffers[token] = updateOffer{names: names, expires: now.Add(updateOfferTTL)}
	a.updateMu.Unlock()

	var errText string
	if checkErr != nil {
		errText = checkErr.Error()
	}
	markup := models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{
		Text:         render.UpdateNow,
		CallbackData: updateCallbackPrefix + token,
	}}}}
	return render.PluginUpdatesAvailable(lines, updateOfferValidFor, errText), markup
}

// takeUpdateOffer 取出并作废 token 对应的更新；不存在或已过期时返回 false。
func (a *App) takeUpdateOffer(token string) (updateOffer, bool) {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	offer, ok := a.updateOffers[token]
	delete(a.updateOffers, token)
	return offer, ok && time.Now().Before(offer.expires)
}

func (a *App) handleCallbackUpdate(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	query := update.CallbackQuery
	msg := query.Message.Message
	report := func(text string) {
		var err error
		if msg != nil {
			err = a.editMessage(ctx, msg.Chat.ID, msg.ID, text, emptyKeyboard())
		} else {
			_, err = a.sendMessage(ctx, query.From.ID, text, 0)
		}
		if err != nil {
			a.log.Errorln(err)
		}
	}

	if !a.updating.CompareAndSwap(false, true) {
		a.answerCallback(ctx, query.ID, render.UpdateBusy)
		return
	}
	defer a.updating.Store(false)

	offer, ok := a.takeUpdateOffer(strings.TrimPrefix(query.Data, updateCallbackPrefix))
	if !ok {
		a.answerCallback(ctx, query.ID, render.UpdateExpired)
		if msg != nil {
			if _, err := a.tg.EditMessageReplyMarkup(ctx, &tgb.EditMessageReplyMarkupParams{
				ChatID:      msg.Chat.ID,
				MessageID:   msg.ID,
				ReplyMarkup: emptyKeyboard(),
			}); err != nil {
				a.log.Errorln(err)
			}
		}
		return
	}
	a.answerCallback(ctx, query.ID, "")
	report(render.UpdatingPlugins)

	applyCtx, cancel := context.WithTimeout(ctx, updateApplyTimeout)
	defer cancel()
	var updated, failed []string
	for _, name := range offer.names {
		u, err := a.plugins.ApplyUpdate(applyCtx, name)
		switch {
		case err == nil:
			updated = append(updated, render.PluginUpdateLine(u.DisplayName, u.Name,
				render.PluginVersion(u.Current.Version, u.Current.BuildTime),
				render.PluginVersion(u.Latest.Version, u.Latest.BuildTime)))
			a.log.Infof("插件 %s 已更新到 %s（%s）", name, u.Latest.Version, u.Latest.BuildTime)
		case errors.Is(err, plugin.ErrUpToDate):
			failed = append(failed, render.PluginUpdateFailedLine(a.pluginDisplayName(name), name, err.Error()))
		default:
			a.log.Errorf("更新插件 %s 失败: %v", name, err)
			failed = append(failed, render.PluginUpdateFailedLine(a.pluginDisplayName(name), name, err.Error()))
		}
	}
	report(render.PluginUpdateResult(updated, failed))
	if len(updated) > 0 {
		a.requestReload()
	}
}

// notifyRollbacks 告知管理员本次启动中因新版本加载失败而回滚的插件。
func (a *App) notifyRollbacks(ctx context.Context) {
	for _, rb := range a.plugins.Rollbacks() {
		text := render.PluginRolledBack(a.pluginDisplayName(rb.Name), rb.Name,
			render.PluginVersion(rb.Version, rb.BuildTime), rb.Err.Error())
		for _, adminID := range a.cfg.Bot.Admin {
			if _, err := a.sendMessage(ctx, adminID, text, 0); err != nil {
				a.log.Warnf("向管理员 %d 发送插件回滚通知失败: %v", adminID, err)
			}
		}
	}
}

func (a *App) pluginDisplayName(name string) string {
	for _, info := range a.plugins.Info() {
		if info.Name == name {
			return info.DisplayName
		}
	}
	return name
}

func newUpdateToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
