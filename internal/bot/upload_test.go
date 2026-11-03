package bot

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/config"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

type capturedForm struct {
	fields        map[string]string
	files         map[string]string // 字段名 -> 文件名:内容
	contentLength int64
	bodyLen       int64
}

// captureServer 记录 multipart 请求的字段与文件，并以 respond 作答。
func captureServer(t *testing.T, respond string) (*httptest.Server, *[]capturedForm) {
	t.Helper()
	var forms []capturedForm
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		form := capturedForm{fields: map[string]string{}, files: map[string]string{}, contentLength: r.ContentLength, bodyLen: int64(len(body))}
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			for k, v := range r.MultipartForm.Value {
				form.fields[k] = v[0]
			}
			for k, fhs := range r.MultipartForm.File {
				f, _ := fhs[0].Open()
				data, _ := io.ReadAll(f)
				f.Close()
				form.files[k] = fhs[0].Filename + ":" + string(data)
			}
		}
		forms = append(forms, form)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respond))
	}))
	t.Cleanup(srv.Close)
	return srv, &forms
}

func uploadTestApp(apiURL string) *App {
	cfg := config.Default()
	cfg.Bot.Token = "123:SECRET"
	cfg.Bot.APIURL = apiURL
	return &App{cfg: cfg, uploadClient: http.DefaultClient}
}

// 流式上传与 go-telegram/bot 发出的表单字段、文件内容一致。
func TestSendAudioFileMatchesLibrary(t *testing.T) {
	srv, forms := captureServer(t, `{"ok":true,"result":{"message_id":7,"date":0,"chat":{"id":42,"type":"private"}}}`)
	dir := t.TempDir()
	audio := filepath.Join(dir, "song.flac")
	thumb := filepath.Join(dir, "cover.jpg")
	_ = os.WriteFile(audio, []byte("AUDIO-BYTES"), 0o644)
	_ = os.WriteFile(thumb, []byte("THUMB"), 0o644)
	params := func() *tgb.SendAudioParams {
		return &tgb.SendAudioParams{
			ChatID:          int64(42),
			Caption:         "「晴天」- 周杰伦\n<b>x</b>",
			ParseMode:       models.ParseModeHTML,
			Title:           "晴天",
			Performer:       "周杰伦",
			Duration:        269,
			ReplyMarkup:     models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{Text: "open", URL: "https://music.163.com/song/186016"}}}},
			ReplyParameters: &models.ReplyParameters{MessageID: 3, AllowSendingWithoutReply: true},
		}
	}

	tg, err := tgb.New("123:SECRET", tgb.WithServerURL(srv.URL), tgb.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	lp := params()
	af, _ := os.Open(audio)
	tf, _ := os.Open(thumb)
	lp.Audio = &models.InputFileUpload{Filename: "song.flac", Data: af}
	lp.Thumbnail = &models.InputFileUpload{Filename: "cover.jpg", Data: tf}
	if _, err := tg.SendAudio(context.Background(), lp); err != nil {
		t.Fatal(err)
	}
	af.Close()
	tf.Close()

	msg, err := uploadTestApp(srv.URL).sendAudioFile(context.Background(), params(), audio, thumb, nil)
	if err != nil || msg.ID != 7 {
		t.Fatalf("msg = %+v, err = %v", msg, err)
	}

	lib, ours := (*forms)[0], (*forms)[1]
	if !maps.Equal(lib.fields, ours.fields) {
		t.Errorf("表单字段不一致:\nlibrary: %v\nstream:  %v", lib.fields, ours.fields)
	}
	if !maps.Equal(lib.files, ours.files) {
		t.Errorf("文件不一致:\nlibrary: %v\nstream:  %v", lib.files, ours.files)
	}
	if ours.contentLength != ours.bodyLen {
		t.Errorf("Content-Length = %d，实际请求体 %d 字节", ours.contentLength, ours.bodyLen)
	}
}

func TestUploadErrors(t *testing.T) {
	for _, c := range []struct {
		resp  string
		check func(error) bool
	}{
		{`{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`, func(err error) bool { return errors.Is(err, tgb.ErrorForbidden) }},
		{`{"ok":false,"error_code":400,"description":"Bad Request: not enough rights to send text messages to the chat"}`, func(err error) bool {
			return errors.Is(err, tgb.ErrorBadRequest) && strings.Contains(err.Error(), "not enough rights to send")
		}},
		{`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 12","parameters":{"retry_after":12}}`, func(err error) bool {
			tooMany, ok := errors.AsType[*tgb.TooManyRequestsError](err)
			return ok && tooMany.RetryAfter == 12 && strings.Contains(err.Error(), "Too Many Requests")
		}},
	} {
		srv, _ := captureServer(t, c.resp)
		path := filepath.Join(t.TempDir(), "a.mp3")
		_ = os.WriteFile(path, []byte("x"), 0o644)
		_, err := uploadTestApp(srv.URL).sendAudioFile(context.Background(), &tgb.SendAudioParams{ChatID: int64(1)}, path, "", nil)
		if err == nil || !c.check(err) {
			t.Errorf("响应 %s 得到 err = %v", c.resp, err)
		}
	}

	// 网络错误中不暴露 token
	srv, _ := captureServer(t, "")
	srv.Close()
	path := filepath.Join(t.TempDir(), "a.mp3")
	_ = os.WriteFile(path, []byte("x"), 0o644)
	_, err := uploadTestApp(srv.URL).sendAudioFile(context.Background(), &tgb.SendAudioParams{ChatID: int64(1)}, path, "", nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("err = %v，不应包含 token", err)
	}
}

func TestUploadProgress(t *testing.T) {
	srv, forms := captureServer(t, `{"ok":true,"result":{"message_id":1,"date":0,"chat":{"id":1,"type":"private"}}}`)
	path := filepath.Join(t.TempDir(), "a.flac")
	_ = os.WriteFile(path, make([]byte, 256<<10), 0o644)
	var last, total int64
	calls := 0
	_, err := uploadTestApp(srv.URL).sendAudioFile(context.Background(), &tgb.SendAudioParams{ChatID: int64(1)}, path, "", func(s, tot int64) {
		if s < last {
			t.Errorf("进度回退: %d -> %d", last, s)
		}
		last, total = s, tot
		calls++
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls < 2 || last != total || total != (*forms)[0].bodyLen {
		t.Errorf("calls = %d, sent = %d, total = %d, body = %d", calls, last, total, (*forms)[0].bodyLen)
	}
}

func TestWatchUpload(t *testing.T) {
	old := uploadStatusInterval
	uploadStatusInterval = 20 * time.Millisecond
	defer func() { uploadStatusInterval = old }()

	var mu sync.Mutex
	var texts []string
	status := func(s string) error {
		mu.Lock()
		texts = append(texts, s)
		mu.Unlock()
		return nil
	}
	a := &App{}
	progress, stop := a.watchUpload(status, store.SongInfo{SongName: "晴天", FileExt: "flac"})
	progress(10<<20, 40<<20)
	time.Sleep(50 * time.Millisecond)
	progress(40<<20, 40<<20)
	time.Sleep(100 * time.Millisecond)
	stop()
	n := len(texts)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(texts) != n {
		t.Error("stop 之后仍在更新状态")
	}
	joined := strings.Join(texts, "\n---\n")
	if !strings.Contains(joined, render.UploadProgress) || !strings.Contains(joined, "[=====>              ]  25%</code>\n<code>10.00/40.00 MB") {
		t.Errorf("缺少上传进度: %s", joined)
	}
	if strings.Count(joined, render.UploadWaiting) != 1 || !strings.HasSuffix(texts[len(texts)-1], render.UploadWaiting) {
		t.Errorf("上传完成后应只提示一次等待处理: %s", joined)
	}
}

func TestWatchUploadShowsFirstFrameImmediately(t *testing.T) {
	old := uploadStatusInterval
	uploadStatusInterval = time.Hour
	defer func() { uploadStatusInterval = old }()

	shown := make(chan string, 1)
	a := &App{}
	progress, stop := a.watchUpload(func(s string) error {
		shown <- s
		return nil
	}, store.SongInfo{SongName: "散歩道"})
	defer stop()
	progress(0, 40<<20)
	select {
	case s := <-shown:
		if !strings.Contains(s, "  0%") {
			t.Errorf("首帧应为 0%%: %s", s)
		}
	case <-time.After(time.Second):
		t.Fatal("开始上传后未立即显示进度")
	}
}
