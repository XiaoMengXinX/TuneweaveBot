package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
)

func TestHelpListsUserCommandsOnly(t *testing.T) {
	text := newTestApp(nil).helpText()
	for _, want := range []string{"/music &lt;来源&gt;", "<code>/music netease 晴天</code>", "/search [来源]", "/lyric [来源]",
		"/recognize", "/status", "/about", "/help", "<code>netease</code> / <code>ne</code>（默认）", "@testbot"} {
		if !strings.Contains(text, want) {
			t.Errorf("帮助缺少 %q:\n%s", want, text)
		}
	}
	for _, admin := range []string{"/reload", "/login", "/rmcache", "/plugin", "/checkupdate", "/setcachechat"} {
		if strings.Contains(text, admin) {
			t.Errorf("帮助不应包含管理员命令 %s", admin)
		}
	}
}

func commandMessage(text string) *models.Message {
	cmd := strings.SplitN(text, " ", 2)[0]
	return &models.Message{
		ID:       10,
		Chat:     models.Chat{ID: 7, Type: models.ChatTypePrivate},
		From:     &models.User{ID: 7},
		Text:     text,
		Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: len(cmd)}},
	}
}

// /music 与 /lyric 的首个参数是来源时，只在该来源中搜索剩余关键词。
func TestMusicAndLyricProviderPrefix(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(a *App, m *models.Message)
		text string
	}{
		{"music", func(a *App, m *models.Message) { a.handleMusic(context.Background(), nil, &models.Update{Message: m}) }, "/music ne 晴天"},
		{"lyric", func(a *App, m *models.Message) { a.handleLyric(context.Background(), nil, &models.Update{Message: m}) }, "/lyric netease 晴天"},
	} {
		tg, _ := newTestBot(t)
		a := newTestApp(tg)
		p := newFakeProvider()
		var got []string
		p.searchFn = func(_ context.Context, keyword string, _, _ int) ([]provider.TrackSummary, int, error) {
			got = append(got, keyword)
			return nil, 0, nil
		}
		setTestProvider(t, a, p)
		c.run(a, commandMessage(c.text))
		if len(got) != 1 || got[0] != "晴天" {
			t.Errorf("%s: 搜索关键词 = %q，应去掉来源前缀", c.name, got)
		}
	}
}
