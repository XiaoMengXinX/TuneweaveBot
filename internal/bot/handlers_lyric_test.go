package bot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

func TestLyricFromReplyButton(t *testing.T) {
	var document, caption, parseMode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sendDocument") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			if _, header, err := r.FormFile("document"); err == nil {
				document = header.Filename
			}
			caption, parseMode = r.FormValue("caption"), r.FormValue("parse_mode")
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":10,"chat":{"id":-100123,"type":"supergroup"}}}`)
	}))
	t.Cleanup(srv.Close)
	tg, err := tgb.New("1:test", tgb.WithServerURL(srv.URL), tgb.WithSkipGetMe(), tgb.WithNotAsyncHandlers())
	testBotURLs.Store(tg, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	a := newTestApp(tg)
	a.cacheDir = t.TempDir()
	p := newFakeProvider()
	p.searchFn = func(context.Context, string, int, int) ([]provider.TrackSummary, int, error) {
		t.Error("reply with song button must not search")
		return nil, 0, nil
	}
	p.lyricsFn = func(_ context.Context, ref provider.Ref) (*provider.Lyric, error) {
		if ref.ID != "123" {
			t.Errorf("lyrics ref = %+v, want song 123", ref)
		}
		return &provider.Lyric{
			LRC:     "[00:00.00]作词 : A\n[00:01.00]\n[00:02.00]lyric <1>",
			Title:   "Song",
			Artists: []string{"Artist", "Other"},
		}, nil
	}
	setTestProvider(t, a, p)
	keyboard := musicKeyboard(store.SongInfo{Provider: "netease", TrackID: "123", SongName: "Song", SongArtists: "Artist", ShareText: "https://music.163.com/song?id=123"}, nil)
	a.handleLyric(t.Context(), tg, &models.Update{Message: &models.Message{
		ID:       2,
		Text:     "/lyric",
		Chat:     models.Chat{ID: -100123, Type: models.ChatTypeSupergroup},
		Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Length: len("/lyric")}},
		ReplyToMessage: &models.Message{
			ID:          1,
			Chat:        models.Chat{ID: -100123, Type: models.ChatTypeSupergroup},
			Audio:       &models.Audio{FileID: "audio"},
			ReplyMarkup: &keyboard,
		},
	}})
	if document != songFileName("Artist/Other", "Song", "lrc") || document != "Artist,Other - Song.lrc" {
		t.Errorf("document = %q, want lyric file named like the song file", document)
	}
	if want := render.LyricCaption("作词 : A\nlyric <1>"); caption != want || parseMode != string(models.ParseModeHTML) {
		t.Errorf("caption = %q (%s), want %q", caption, parseMode, want)
	}
}

func TestLyricRejectsProgram(t *testing.T) {
	const link = "https://music.163.com/program?id=2061"
	for _, tt := range []struct {
		name  string
		text  string
		reply *models.Message
	}{
		{name: "argument", text: "/lyric " + link},
		{name: "reply", text: "/lyric", reply: &models.Message{ID: 1, Chat: models.Chat{ID: 42}, Text: link}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var texts []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
				}
				texts = append(texts, r.FormValue("text"))
				_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":10,"chat":{"id":42,"type":"private"}}}`)
			}))
			t.Cleanup(srv.Close)
			tg, err := tgb.New("1:test", tgb.WithServerURL(srv.URL), tgb.WithSkipGetMe(), tgb.WithNotAsyncHandlers())
			testBotURLs.Store(tg, srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			a := newTestApp(tg)
			p := newFakeProvider()
			p.lyricsFn = func(context.Context, provider.Ref) (*provider.Lyric, error) {
				t.Error("program link must not fetch lyrics")
				return nil, errors.New("unexpected lyrics call")
			}
			p.searchFn = func(context.Context, string, int, int) ([]provider.TrackSummary, int, error) {
				t.Error("program link must not search")
				return nil, 0, nil
			}
			setTestProvider(t, a, p)
			a.handleLyric(t.Context(), tg, &models.Update{Message: &models.Message{
				ID:             2,
				Text:           tt.text,
				Chat:           models.Chat{ID: 42, Type: models.ChatTypePrivate},
				Entities:       []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Length: len("/lyric")}},
				ReplyToMessage: tt.reply,
			}})
			if want := []string{render.FetchingLyric, render.LrcNoProgram}; strings.Join(texts, "|") != strings.Join(want, "|") {
				t.Errorf("texts = %q, want %q", texts, want)
			}
		})
	}
}
