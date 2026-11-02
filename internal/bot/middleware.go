package bot

import (
	"context"
	"runtime/debug"
	"slices"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func (a *App) recoverMiddleware(next tgb.HandlerFunc) tgb.HandlerFunc {
	return func(ctx context.Context, tg *tgb.Bot, update *models.Update) {
		// 登记在途 handler，退出/重载时等它们结束再关闭 store 与插件
		a.inflight.Add(1)
		defer a.inflight.Done()
		defer func() {
			if r := recover(); r != nil {
				a.log.Errorf("panic: %v\n%s", r, debug.Stack())
			}
		}()
		if a.workCtx != nil {
			ctx = a.workCtx
		}
		next(ctx, tg, update)
	}
}

func (a *App) adminOnly(next tgb.HandlerFunc) tgb.HandlerFunc {
	return func(ctx context.Context, tg *tgb.Bot, update *models.Update) {
		var userID int64
		switch {
		case update.Message != nil && update.Message.From != nil:
			userID = update.Message.From.ID
		case update.CallbackQuery != nil:
			userID = update.CallbackQuery.From.ID
		default:
			return
		}
		if !slices.Contains(a.cfg.Bot.Admin, userID) {
			return
		}
		next(ctx, tg, update)
	}
}
