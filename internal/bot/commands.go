package bot

import (
	"context"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// chat scope 覆盖 default scope，管理员菜单须包含公共指令
func (a *App) setupCommands(ctx context.Context, tg *tgb.Bot) {
	userCommands := []models.BotCommand{
		{Command: "music", Description: "点歌（ID 或关键词）"},
		{Command: "search", Description: "搜索歌曲（可指定 provider）"},
		{Command: "lyric", Description: "获取歌词"},
		{Command: "recognize", Description: "听歌识曲"},
		{Command: "status", Description: "缓存与插件状态"},
		{Command: "about", Description: "关于本 bot"},
		{Command: "help", Description: "使用帮助"},
	}
	adminOnly := []models.BotCommand{
		{Command: "login", Description: "登录插件账号"},
		{Command: "rmcache", Description: "清除歌曲缓存"},
		{Command: "reload", Description: "重载配置与插件"},
		{Command: "checkupdate", Description: "检查插件更新"},
		{Command: "plugin", Description: "查看插件状态与 manifest"},
		{Command: "setcachechat", Description: "设置 Guest Mode 缓存群"},
	}

	// 插件命令（/netease、/program 等）由 manifest 声明。内置命令（含管理员命令）
	// 先占位，避免插件命令与保留名冲突，也避免菜单超过 Telegram 的 100 条上限。
	if a.plugins != nil {
		seen := make(map[string]bool, len(userCommands)+len(adminOnly))
		for _, cmd := range userCommands {
			seen[cmd.Command] = true
		}
		for _, cmd := range adminOnly {
			seen[cmd.Command] = true
		}
		// 预留管理员命令的名额，管理员菜单 = 用户命令 + 插件命令 + 管理员命令
		maxUserCommands := 100 - len(adminOnly)
		for _, pc := range a.plugins.Commands() {
			name := pc.Command.Name
			if seen[name] || len(userCommands) >= maxUserCommands {
				continue
			}
			seen[name] = true
			userCommands = append(userCommands, models.BotCommand{Command: name, Description: pc.Command.Description})
		}
	}
	if _, err := tg.SetMyCommands(ctx, &tgb.SetMyCommandsParams{
		Commands: userCommands,
		Scope:    &models.BotCommandScopeDefault{},
	}); err != nil {
		a.log.Warnf("注册指令菜单失败: %v", err)
	}

	adminCommands := append(append([]models.BotCommand{}, userCommands...), adminOnly...)
	for _, adminID := range a.cfg.Bot.Admin {
		if _, err := tg.SetMyCommands(ctx, &tgb.SetMyCommandsParams{
			Commands: adminCommands,
			Scope:    &models.BotCommandScopeChat{ChatID: adminID},
		}); err != nil {
			a.log.Warnf("注册管理员指令失败 (chat %d): %v（该管理员可能尚未与 bot 私聊过，私聊一次后重启即可）", adminID, err)
		}
	}
}
