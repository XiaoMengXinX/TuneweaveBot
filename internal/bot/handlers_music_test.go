package bot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"gorm.io/gorm/logger"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

func TestMessageRef(t *testing.T) {
	audioKeyboard := musicKeyboard(store.SongInfo{MusicID: 123, SongName: "Song", SongArtists: "Artist", PageURL: songLink(123)}, nil)
	guestKeyboard := linkKeyboard("网易云音乐", songLink(456))
	for _, tt := range []struct {
		name    string
		message *models.Message
		wantID  string
		wantSub string
		wantOK  bool
	}{
		{name: "text", message: &models.Message{Text: "分享 https://music.163.com/song?id=123&userid=1"}, wantID: "123", wantOK: true},
		{name: "caption", message: &models.Message{Caption: "https://music.163.com/#/song?id=123"}, wantID: "123", wantOK: true},
		{name: "caption text link", message: &models.Message{
			Caption:         "点我",
			CaptionEntities: []models.MessageEntity{{Type: models.MessageEntityTypeTextLink, URL: "https://music.163.com/song?id=123"}},
		}, wantID: "123", wantOK: true},
		{name: "audio button", message: &models.Message{Caption: "「Song」- Artist\nvia @testbot", ReplyMarkup: &audioKeyboard}, wantID: "123", wantOK: true},
		{name: "song link button", message: &models.Message{Text: "已识别到歌曲", ReplyMarkup: &guestKeyboard}, wantID: "456", wantOK: true},
		{name: "text before button", message: &models.Message{Text: "https://music.163.com/song?id=789", ReplyMarkup: &audioKeyboard}, wantID: "789", wantOK: true},
		{name: "program", message: &models.Message{Text: "https://music.163.com/program?id=2061"}, wantID: "2061", wantSub: "program", wantOK: true},
		{name: "program before button", message: &models.Message{Text: "https://music.163.com/dj?id=2061", ReplyMarkup: &audioKeyboard}, wantID: "2061", wantSub: "program", wantOK: true},
		{name: "no link", message: &models.Message{Text: "雨き声残響 2", Entities: []models.MessageEntity{{Type: models.MessageEntityTypeURL}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestApp(nil)
			ref, ok := a.messageRef(context.Background(), tt.message)
			if ok != tt.wantOK {
				t.Fatalf("messageRef ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if ref.ID != tt.wantID || ref.Sub != tt.wantSub || ref.Provider != "netease" {
				t.Errorf("messageRef = %+v, want id %q sub %q", ref, tt.wantID, tt.wantSub)
			}
		})
	}
}

func TestTemplateCommand(t *testing.T) {
	tg, rec := newTestBot(t)
	a := newTestApp(tg)
	var matched string
	p := newFakeProvider()
	p.matchFn = func(_ context.Context, text string) (*provider.Ref, bool) {
		matched = text
		return fakeMatch(text)
	}
	p.canonicalizeFn = func(_ context.Context, ref provider.Ref) (provider.Ref, error) {
		return provider.Ref{Provider: ref.Provider, ID: "123", URL: ref.URL}, nil
	}
	setTestProvider(t, a, p)
	st, err := store.Open(filepath.Join(t.TempDir(), "cache.db"), logger.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a.store = st
	if err := st.Save(&store.SongInfo{MusicID: 123, SongName: "Song", FileID: "cached-audio"}); err != nil {
		t.Fatal(err)
	}
	a.handleTemplateMusic("netease", "https://music.163.com/program?id={args}")(t.Context(), tg, &models.Update{Message: &models.Message{
		ID:       1,
		Text:     "/program 2061",
		Chat:     models.Chat{ID: 42, Type: models.ChatTypePrivate},
		Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Length: len("/program")}},
	}})
	if matched != "https://music.163.com/program?id=2061" {
		t.Errorf("matched text = %q", matched)
	}
	if !rec.has("/sendAudio") {
		t.Error("template command should send cached audio")
	}
}

func TestProcessCachedProgramKeyboard(t *testing.T) {
	var keyboards []models.InlineKeyboardMarkup
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendAudio"):
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			var keyboard models.InlineKeyboardMarkup
			if err := json.Unmarshal([]byte(r.FormValue("reply_markup")), &keyboard); err != nil {
				t.Error(err)
			}
			keyboards = append(keyboards, keyboard)
		case strings.HasSuffix(r.URL.Path, "/deleteMessage"):
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":10,"chat":{"id":42,"type":"private"}}}`)
	}))
	t.Cleanup(srv.Close)
	tg, err := tgb.New("1:test", tgb.WithServerURL(srv.URL), tgb.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	a := newTestApp(tg)
	p := newFakeProvider()
	p.canonicalizeFn = func(_ context.Context, ref provider.Ref) (provider.Ref, error) {
		if ref.Sub == "program" && ref.ID == "2061" {
			return provider.Ref{Provider: ref.Provider, ID: "123", URL: ref.URL}, nil
		}
		return ref, nil
	}
	setTestProvider(t, a, p)
	st, err := store.Open(filepath.Join(t.TempDir(), "cache.db"), logger.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a.store = st
	if err := st.Save(&store.SongInfo{MusicID: 123, SongName: "Song", FileID: "cached-audio"}); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []provider.Ref{
		{Provider: "netease", ID: "123"},
		{Provider: "netease", ID: "2061", Sub: "program"},
	} {
		if err := a.processMusic(t.Context(), ref, &models.Message{ID: 1, Chat: models.Chat{ID: 42}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(keyboards) != 2 {
		t.Fatalf("sent audio = %d, want 2", len(keyboards))
	}
	for i, want := range []string{"https://music.163.com/song?id=123", programLink(2061)} {
		if got := keyboards[i].InlineKeyboard[0][0].URL; got != want {
			t.Errorf("link = %q, want %q", got, want)
		}
		if got := *keyboards[i].InlineKeyboard[1][0].SwitchInlineQuery; got != "https://music.163.com/song?id=123" {
			t.Errorf("inline query = %q, want song link", got)
		}
	}
}
