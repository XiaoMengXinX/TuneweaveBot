package bot

import (
	"context"
	"strconv"
	"strings"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/config"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
)

func (a *App) handleAbout(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	if _, err := a.reply(ctx, m, render.About(a.build.RuntimeVer, a.build.VersionName, a.build.CommitSHA, a.build.BuildTime, a.build.BuildArch)); err != nil {
		a.log.Errorln(err)
	}
}

func (a *App) handleStatus(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	if err := a.statusSem.Acquire(ctx, 1); err != nil {
		return
	}
	defer func() {
		time.Sleep(500 * time.Millisecond)
		a.statusSem.Release(1)
	}()

	total, byUser, byChat := a.store.Counts(m.From.ID, m.Chat.ID)
	var text strings.Builder
	text.WriteString(render.StatusInfo(m.Chat.Title, m.Chat.Username, m.Chat.ID, m.From.ID, total, byChat, byUser))
	if a.plugins != nil {
		for _, info := range a.plugins.Info() {
			text.WriteString("\n")
			text.WriteString(render.PluginLine(info.DisplayName, info.Name, info.Version, strings.Join(info.Capabilities, "/")))
		}
	}
	for _, name := range a.auth.Names() {
		text.WriteString("\n")
		text.WriteString(render.AuthLine(name, a.auth.Available(name)))
	}
	if _, err := a.reply(ctx, m, text.String()); err != nil {
		a.log.Errorln(err)
	}
}

func (a *App) handleRmCache(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	args := commandArguments(m)
	var ref *provider.Ref
	if args == "" && m.ReplyToMessage != nil {
		ref, _ = a.messageRef(ctx, m.ReplyToMessage)
	} else if args != "" {
		if parsed, ok := a.parseRefArg(args); ok {
			ref = &parsed
		} else if matched, _, ok := a.providers.Match(ctx, args); ok {
			ref = matched
		}
	}
	if ref == nil {
		text := render.NoResults
		if args == "" {
			text = render.InputIDorLink
		}
		if _, err := a.reply(ctx, m, text); err != nil {
			a.log.Errorln(err)
		}
		return
	}
	canonical, err := a.providers.Canonicalize(ctx, *ref)
	if err != nil {
		a.log.Errorln(err)
		if _, err := a.reply(ctx, m, render.NoResults); err != nil {
			a.log.Errorln(err)
		}
		return
	}
	name, ok, err := a.store.DeleteByRef(canonical.Provider, canonical.ID)
	if err != nil {
		a.log.Errorln(err)
		if _, rErr := a.reply(ctx, m, render.ProcessFailed); rErr != nil {
			a.log.Errorln(rErr)
		}
		return
	}
	text := render.NoCache
	if ok {
		text = render.RmcacheReport(name)
	}
	if _, err := a.reply(ctx, m, text); err != nil {
		a.log.Errorln(err)
	}
}

func (a *App) handleReload(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	if _, err := a.reply(ctx, m, render.Reloading); err != nil {
		a.log.Errorln(err)
	}
	a.reload(ErrReload)
}

// handleSetCacheChat 无参数时设为当前聊天，参数为 0 时关闭
func (a *App) handleSetCacheChat(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	chatID := m.Chat.ID
	if args := strings.TrimSpace(commandArguments(m)); args != "" {
		id, err := strconv.ParseInt(args, 10, 64)
		if err != nil {
			if _, err := a.reply(ctx, m, render.InputChatID); err != nil {
				a.log.Errorln(err)
			}
			return
		}
		chatID = id
	}
	if chatID != 0 && chatID != m.Chat.ID {
		if _, err := a.sendMessage(ctx, chatID, render.CacheChatTest, 0); err != nil {
			a.log.Errorln(err)
			if _, err := a.reply(ctx, m, render.CacheChatUnreachable); err != nil {
				a.log.Errorln(err)
			}
			return
		}
	}

	a.cacheChat.Store(chatID)
	text := render.CacheChatDisabled
	if chatID != 0 {
		text = render.CacheChatSet(chatID)
	}
	if err := config.UpdateCacheChat(a.cfgPath, chatID); err != nil {
		a.log.Errorln(err)
		text += "\n配置文件写入失败: " + render.Escape(err.Error()) + "\n本次设置已生效，重启后将失效"
	}
	if _, err := a.reply(ctx, m, text); err != nil {
		a.log.Errorln(err)
	}
}
