package bot

import (
	"context"
	"strconv"
	"strings"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/config"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/plugin"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
)

func (a *App) handleAbout(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	var plugins []string
	if a.plugins != nil {
		for _, info := range a.plugins.Info() {
			plugins = append(plugins, info.Name)
		}
	}
	if _, err := a.reply(ctx, m, render.About(a.build.RuntimeVer, a.build.VersionName, a.build.CommitSHA, a.build.BuildTime, a.build.BuildArch, plugins)); err != nil {
		a.log.Errorln(err)
	}
}

func (a *App) handleHelp(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	if _, err := a.reply(ctx, update.Message, a.helpText()); err != nil {
		a.log.Errorln(err)
	}
}

// helpText 按已安装的 provider 与插件命令生成帮助。
func (a *App) helpText() string {
	var sources []render.HelpSource
	if a.providers != nil {
		for _, p := range a.providers.Enabled() {
			sources = append(sources, render.HelpSource{
				Name:        p.Name(),
				DisplayName: p.DisplayName(),
				Code:        p.Code(),
				Default:     p.Name() == a.providers.DefaultName(),
			})
		}
	}
	var commands []render.HelpCommand
	if a.plugins != nil {
		for _, pc := range a.plugins.Commands() {
			usage := pc.Command.Usage
			if usage == "" {
				usage = "<ID/关键词>"
				if pc.Command.CommandType() == "template" {
					usage = "<参数>"
				}
			}
			commands = append(commands, render.HelpCommand{Name: pc.Command.Name, Usage: usage, Description: pc.Command.Description})
		}
	}
	return render.Help(sources, commands, a.me.Username)
}

// handlePlugin 无参数时列出插件状态，带插件名时显示其 manifest。
func (a *App) handlePlugin(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	if a.plugins == nil {
		return
	}
	text := a.pluginStatusText()
	if name := strings.TrimSpace(commandArguments(m)); name != "" {
		text = render.PluginNotFound(name)
		if p, ok := a.providers.Lookup(name); ok {
			name = p.Name()
		}
		if manifest, ok := a.plugins.ManifestJSON(name); ok {
			text = render.PluginManifest(a.pluginDisplayName(name), name, manifest)
		}
	}
	if _, err := a.reply(ctx, m, text); err != nil {
		a.log.Errorln(err)
	}
}

func (a *App) pluginStatusText() string {
	var items []render.PluginStatus
	for _, info := range a.plugins.Info() {
		status := render.PluginStatus{
			Name:         info.Name,
			DisplayName:  info.DisplayName,
			Version:      info.Version,
			Build:        plugin.BuildLabel(info.Commit, info.BuildTime),
			Capabilities: info.Capabilities,
			Instances:    info.Instances,
			Default:      a.providers != nil && info.Name == a.providers.DefaultName(),
			Updatable:    info.Updatable,
			Path:         info.Path,
		}
		if _, ok := a.auth.Get(info.Name); ok {
			status.Auth = "正常"
			if !a.auth.Available(info.Name) {
				status.Auth = "已失效，新歌下载暂停"
			}
		}
		items = append(items, status)
	}
	return render.PluginStatusList(items)
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
		if infos := a.plugins.Info(); len(infos) > 0 {
			text.WriteString("\n<b>[插件]</b>")
			for _, info := range infos {
				text.WriteString("\n" + render.PluginLine(info.DisplayName, info.Name, info.Version, plugin.BuildLabel(info.Commit, info.BuildTime)))
			}
		}
	}
	if names := a.auth.Names(); len(names) > 0 {
		text.WriteString("\n<b>[登录]</b>")
		for _, name := range names {
			text.WriteString("\n" + render.AuthLine(name, a.auth.Available(name)))
		}
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
	a.requestReload()
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
