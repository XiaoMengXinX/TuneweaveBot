package bot

import (
	"context"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/config"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
)

const (
	cookieCheckInterval = 10 * time.Minute
	cookieWarnInterval  = 10 * time.Minute
)

func (a *App) watchCookie(ctx context.Context) {
	ticker := time.NewTicker(cookieCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, name := range a.auth.Names() {
				auth, ok := a.auth.Get(name)
				if !ok {
					continue
				}
				status, err := auth.Status(ctx)
				if err != nil {
					// 网络错误不视为凭据失效
					continue
				}
				if status.Authenticated {
					a.resumeDownloads(ctx, name)
				} else {
					a.pauseDownloads(ctx, name)
				}
			}
		}
	}
}

func (a *App) pauseDownloads(ctx context.Context, providerName string) {
	if !a.auth.SetAvailable(providerName, false) {
		return
	}
	a.log.Warnf("%s 凭据已失效，暂停新歌下载", providerName)
	a.notifyAdmins(ctx, render.CookieExpiredAdminWarn, nil)

	warnCtx, cancel := context.WithCancel(context.Background())
	a.warnMu.Lock()
	if old := a.warnCancels[providerName]; old != nil {
		old()
	}
	a.warnCancels[providerName] = cancel
	a.warnMu.Unlock()
	go func() {
		ticker := time.NewTicker(cookieWarnInterval)
		defer ticker.Stop()
		for {
			select {
			case <-warnCtx.Done():
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !a.auth.Available(providerName) {
					a.notifyAdmins(ctx, render.CookieExpiredAdminWarn, nil)
				}
			}
		}
	}()
}

func (a *App) resumeDownloads(ctx context.Context, providerName string) {
	a.warnMu.Lock()
	warnCancel := a.warnCancels[providerName]
	delete(a.warnCancels, providerName)
	a.warnMu.Unlock()
	if warnCancel != nil {
		warnCancel()
	}
	resumed := a.auth.SetAvailable(providerName, true)
	if warnCancel != nil && resumed {
		a.log.Printf("%s 凭据已生效，恢复新歌下载", providerName)
		a.notifyAdmins(ctx, render.CookieResumedAdmin, nil)
	}
}

// finishLogin 应用登录凭据。note 是配置写回失败等附加提示，err 非空表示登录未真正生效。
func (a *App) finishLogin(auth provider.Authenticator, state *provider.AuthState) (note string, err error) {
	if err := auth.Apply(context.Background(), state.Credential); err != nil {
		a.log.Errorln(err)
		return "\n应用凭据失败: " + err.Error(), err
	}
	if state.Persist != nil {
		if err := config.UpdatePluginValue(a.cfgPath, state.Persist.Section, state.Persist.Key, state.Credential); err != nil {
			a.log.Errorln(err)
			note = "\n配置文件写入失败: " + err.Error() + "\n本次登录已生效，重启后将失效"
		}
	}
	a.resumeDownloads(context.Background(), auth.Name())
	a.log.Printf("%s 登录成功: %s", auth.Name(), state.Nickname)
	return note, nil
}

func (a *App) notifyAdmins(ctx context.Context, text string, markup models.ReplyMarkup) {
	for _, adminID := range a.cfg.Bot.Admin {
		_, err := a.tg.SendMessage(ctx, &tgb.SendMessageParams{
			ChatID:      adminID,
			Text:        text,
			ParseMode:   models.ParseModeHTML,
			ReplyMarkup: markup,
		})
		if err != nil {
			a.log.Warnf("通知管理员 %d 失败: %v", adminID, err)
		}
	}
}
