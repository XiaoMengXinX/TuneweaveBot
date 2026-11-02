package bot

import (
	"context"
	"testing"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/plugin"
)

func TestUpdateOfferIsSingleUseAndExpires(t *testing.T) {
	a := &App{updateOffers: map[string]updateOffer{}}
	_, markup := a.offerUpdates([]plugin.Update{{Name: "netease", DisplayName: "网易云音乐"}}, nil)
	token := markup.InlineKeyboard[0][0].CallbackData[len(updateCallbackPrefix):]

	offer, ok := a.takeUpdateOffer(token)
	if !ok || len(offer.names) != 1 || offer.names[0] != "netease" {
		t.Fatalf("take = %+v, %v", offer, ok)
	}
	if _, ok := a.takeUpdateOffer(token); ok {
		t.Error("按钮应只能使用一次")
	}

	a.updateOffers["old"] = updateOffer{names: []string{"netease"}, expires: time.Now().Add(-time.Second)}
	if _, ok := a.takeUpdateOffer("old"); ok {
		t.Error("过期按钮不应生效")
	}
}

// 重载只停止轮询：在途 handler 拿到的是 workCtx，不随轮询 ctx 取消。
func TestHandlersSurvivePollingStop(t *testing.T) {
	workCtx, cancelWork := context.WithCancel(context.Background())
	defer cancelWork()
	pollCtx, stopPolling := context.WithCancel(workCtx)
	a := &App{workCtx: workCtx}

	var got context.Context
	h := a.recoverMiddleware(func(ctx context.Context, _ *tgb.Bot, _ *models.Update) { got = ctx })
	stopPolling()
	h(pollCtx, nil, &models.Update{})
	if got.Err() != nil {
		t.Fatal("停止轮询后 handler 的 ctx 不应被取消")
	}
	cancelWork()
	if got.Err() == nil {
		t.Fatal("进程退出时 handler 的 ctx 应被取消")
	}
}
