package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"golang.org/x/sync/semaphore"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
	"gorm.io/gorm/logger"
)

func openVoiceBytes(ctx context.Context, a *App, file *models.File) ([]byte, error) {
	src, cleanup, err := a.openVoice(ctx, file)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return io.ReadAll(src)
}

func TestDownloadVoiceLocal(t *testing.T) {
	tg, rec := newTestBot(t)
	a := newTestApp(tg)
	a.cfg.Bot.LocalMode = true
	original := filepath.Join(t.TempDir(), "voice.ogg")
	content := []byte("voice data")
	if err := os.WriteFile(original, content, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := openVoiceBytes(context.Background(), a, &models.File{FilePath: original})
	if err != nil || string(got) != string(content) {
		t.Fatalf("read voice = %q, error = %v", got, err)
	}
	if rec.has("/file/") {
		t.Fatal("local mode must not request an HTTP download")
	}
	// 读取不应改动原文件
	got, err = os.ReadFile(original)
	if err != nil || string(got) != string(content) {
		t.Fatalf("original voice changed: %q, %v", got, err)
	}
}

func TestDownloadVoiceLocalInvalidPath(t *testing.T) {
	a := newTestApp(nil)
	a.cfg.Bot.LocalMode = true
	dir := t.TempDir()
	for _, path := range []string{"", "voice.ogg", filepath.Join(dir, "missing.ogg"), dir} {
		t.Run(path, func(t *testing.T) {
			if _, err := openVoiceBytes(context.Background(), a, &models.File{FilePath: path}); err == nil {
				t.Fatal("expected local file error")
			}
		})
	}
}

func TestDownloadVoiceHTTP(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/file/bot1:test/voice/voice.ogg" || r.Method != http.MethodGet {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte("voice data"))
			}))
			defer srv.Close()
			tg, err := tgb.New("1:test", tgb.WithServerURL(srv.URL), tgb.WithSkipGetMe())
			testBotURLs.Store(tg, srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			a := newTestApp(tg)
			a.http = srv.Client()
			file := &models.File{FilePath: "voice/voice.ogg"}
			got, err := openVoiceBytes(context.Background(), a, file)
			if status != http.StatusOK {
				if err == nil || !strings.Contains(err.Error(), "404") {
					t.Fatalf("expected HTTP 404 error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "voice data" {
				t.Fatalf("downloaded voice = %q", got)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := openVoiceBytes(ctx, a, file); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected canceled download, got %v", err)
			}
		})
	}
}

func TestProcessMusicCachedInfo(t *testing.T) {
	rec := &requestRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/deleteMessage") {
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()
	tg, err := tgb.New("1:test", tgb.WithServerURL(srv.URL), tgb.WithSkipGetMe())
	testBotURLs.Store(tg, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	a := newTestApp(tg)
	st, err := store.Open(filepath.Join(t.TempDir(), "cache.db"), logger.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a.store = st
	info := store.SongInfo{
		Provider: "netease", TrackID: "123", SongName: "雨き声残響", SongArtists: "ゆめこ",
		SongAlbum: "最新热歌慢摇92", FileID: "cached-audio",
	}
	if err := st.Save(&info); err != nil {
		t.Fatal(err)
	}
	called := false
	err = a.processMusic(context.Background(), provider.Ref{Provider: info.Provider, ID: info.TrackID}, &models.Message{ID: 1, Chat: models.Chat{ID: 42}}, func(got store.SongInfo) {
		called = true
		if got.SongName != info.SongName || got.SongArtists != info.SongArtists || got.SongAlbum != info.SongAlbum {
			t.Errorf("unexpected song info: %+v", got)
		}
		if rec.has("/sendAudio") {
			t.Error("song info should be available before sending audio")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called || !rec.has("/sendAudio") {
		t.Fatal("cached song should provide metadata and send audio")
	}
}

func TestProcessMusicInfoWithoutURL(t *testing.T) {
	for _, tt := range []struct {
		name   string
		code   int
		fee    int
		reason string
		want   string
	}{
		{name: "empty url list", code: 200, want: ": netease 123, no audio url (code 200, fee 0)"},
		{name: "no audio", code: 404, fee: 1, want: ": netease 123, no audio url (code 404, fee 1)"},
		{name: "with reason", code: 404, reason: "无版权或已下架", want: ": netease 123, no audio url: 无版权或已下架 (code 404, fee 0)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tg, rec := newTestBot(t)
			a := newTestApp(tg)
			st, err := store.Open(filepath.Join(t.TempDir(), "cache.db"), logger.Discard)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			a.store = st
			a.auth.SetAvailable("netease", true)
			a.musicSem = semaphore.NewWeighted(1)
			requests := 0
			p := newFakeProvider()
			p.resolveFn = func(_ context.Context, ref provider.Ref) (*provider.Track, error) {
				requests++
				return &provider.Track{
					Ref:     provider.Ref{Provider: fakeProviderName, ID: ref.ID},
					Title:   "雨き声残響",
					Artists: []string{"ゆめこ"},
					Album:   "最新热歌慢摇92",
					Ext:     "mp3",
				}, &provider.NoAudioError{Code: tt.code, Fee: tt.fee, Reason: tt.reason}
			}
			setTestProvider(t, a, p)
			calls := 0
			err = a.processMusic(context.Background(), provider.Ref{Provider: "netease", ID: "123"}, &models.Message{
				ID: 1, Chat: models.Chat{ID: 42}, From: &models.User{ID: 42},
			}, func(info store.SongInfo) {
				calls++
				if info.TrackID != "123" || info.SongName != "雨き声残響" || info.SongArtists != "ゆめこ" || info.SongAlbum != "最新热歌慢摇92" {
					t.Errorf("unexpected song info: %+v", info)
				}
			})
			if err == nil || err.Error() != render.NoAudioLog+tt.want {
				t.Fatalf("err = %v, want %s", err, render.NoAudioLog+tt.want)
			}
			if calls != 1 || requests != 1 {
				t.Errorf("metadata calls = %d, API requests = %d, want 1 each", calls, requests)
			}
			if rec.has("/sendAudio") {
				t.Error("song without download URL should not send audio")
			}
		})
	}
}

func TestRecognizeFailureStatus(t *testing.T) {
	for _, tt := range []struct {
		name string
		want string
	}{
		{name: "get file", want: render.RecognizeReadFailed},
		{name: "download", want: render.RecognizeReadFailed},
		{name: "invalid audio", want: render.RecognizeFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "voice.ogg")
			if err := os.WriteFile(path, []byte("invalid audio"), 0o600); err != nil {
				t.Fatal(err)
			}
			var texts []string
			var methods []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/getFile") {
					if tt.name == "get file" {
						_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"file unavailable"}`))
						return
					}
					filePath := path
					if tt.name == "download" {
						filePath = "relative.ogg"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": models.File{FileID: "voice", FilePath: filePath}})
					return
				}
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
				}
				if strings.HasSuffix(r.URL.Path, "/editMessageText") && r.FormValue("message_id") != "10" {
					t.Errorf("status message ID = %s, want 10", r.FormValue("message_id"))
				}
				texts = append(texts, r.FormValue("text"))
				methods = append(methods, filepath.Base(r.URL.Path))
				_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":10,"chat":{"id":42,"type":"private"}}}`))
			}))
			defer srv.Close()
			tg, err := tgb.New("1:test", tgb.WithServerURL(srv.URL), tgb.WithSkipGetMe())
			testBotURLs.Store(tg, srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			a := newTestApp(tg)
			a.cfg.Bot.LocalMode = true
			a.cacheDir = t.TempDir()
			a.recognizers = []provider.Recognizer{fakeRecognizer{name: fakeProviderName, fn: func(context.Context, []byte, string) (*provider.Ref, error) {
				return nil, errFakeInvalidAudio
			}}}
			a.handleRecognize(context.Background(), tg, &models.Update{Message: &models.Message{
				ReplyToMessage: &models.Message{ID: 1, Chat: models.Chat{ID: 42}, Voice: &models.Voice{FileID: "voice"}},
			}})
			if len(methods) != 2 || methods[0] != "sendMessage" || methods[1] != "editMessageText" {
				t.Errorf("unexpected status methods: %q", methods)
			}
			if len(texts) != 2 || texts[0] != render.Recognizing || texts[1] != tt.want {
				t.Errorf("unexpected status messages: %q", texts)
			}
			entries, err := os.ReadDir(a.cacheDir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary directory left after failure: %v, %v", entries, err)
			}
		})
	}
}
