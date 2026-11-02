package bot

import (
	"context"
	"strings"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// waitToken 登记 token 等待通道并阻塞，直到用户回复提示消息、会话取消或超时。
func (a *App) waitToken(ctx context.Context, chatID int64, promptID int) (string, bool) {
	key := surfaceKey{chatID, promptID}
	ch := make(chan string, 1)
	a.tokenMu.Lock()
	a.tokenWaiters[key] = ch
	a.tokenMu.Unlock()
	defer func() {
		a.tokenMu.Lock()
		delete(a.tokenWaiters, key)
		a.tokenMu.Unlock()
	}()

	select {
	case token := <-ch:
		return token, true
	case <-time.After(tokenLoginBudget):
		return "", false
	case <-ctx.Done():
		return "", false
	}
}

// captureToken 若消息是对某个 token 提示的回复，则投递 token 并删除该消息，返回是否已捕获。
func (a *App) captureToken(ctx context.Context, m *models.Message) bool {
	if m.ReplyToMessage == nil {
		return false
	}
	key := surfaceKey{m.Chat.ID, m.ReplyToMessage.ID}
	a.tokenMu.Lock()
	ch := a.tokenWaiters[key]
	if ch != nil {
		delete(a.tokenWaiters, key)
	}
	a.tokenMu.Unlock()
	if ch == nil {
		return false
	}

	ch <- strings.TrimSpace(m.Text)
	// token 是敏感信息，收到后立即删除用户消息
	if _, err := a.tg.DeleteMessage(ctx, &tgb.DeleteMessageParams{
		ChatID:    m.Chat.ID,
		MessageID: m.ID,
	}); err != nil {
		a.log.Warnf("删除 token 消息失败: %v", err)
	}
	return true
}
