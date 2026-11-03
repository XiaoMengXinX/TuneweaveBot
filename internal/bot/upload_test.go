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
	"testing"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/config"
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

	msg, err := uploadTestApp(srv.URL).sendAudioFile(context.Background(), params(), audio, thumb)
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
		_, err := uploadTestApp(srv.URL).sendAudioFile(context.Background(), &tgb.SendAudioParams{ChatID: int64(1)}, path, "")
		if err == nil || !c.check(err) {
			t.Errorf("响应 %s 得到 err = %v", c.resp, err)
		}
	}

	// 网络错误中不暴露 token
	srv, _ := captureServer(t, "")
	srv.Close()
	path := filepath.Join(t.TempDir(), "a.mp3")
	_ = os.WriteFile(path, []byte("x"), 0o644)
	_, err := uploadTestApp(srv.URL).sendAudioFile(context.Background(), &tgb.SendAudioParams{ChatID: int64(1)}, path, "")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("err = %v，不应包含 token", err)
	}
}
