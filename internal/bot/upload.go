package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// uploadFile 是请求中以文件形式上传的一个字段。
type uploadFile struct {
	field string
	path  string
}

// uploadProgress 在上传请求体被读出时回调，sent 为已发送字节数，total 为请求体总长度。
// 由发送请求的 goroutine 调用，回调应尽快返回。
type uploadProgress func(sent, total int64)

// sendAudioFile 流式上传本地音频及缩略图，内存占用与文件大小无关
// （go-telegram/bot 会把整个请求体读进内存）。progress 可为 nil。
func (a *App) sendAudioFile(ctx context.Context, params *tgb.SendAudioParams, audioPath, thumbPath string, progress uploadProgress) (*models.Message, error) {
	fields := *params
	fields.Audio, fields.Thumbnail = nil, nil
	files := []uploadFile{{field: "audio", path: audioPath}}
	if thumbPath != "" {
		files = append(files, uploadFile{field: "thumbnail", path: thumbPath})
	}
	var msg models.Message
	if err := a.uploadMultipart(ctx, "sendAudio", &fields, files, &msg, progress); err != nil {
		return nil, err
	}
	return &msg, nil
}

// uploadMultipart 流式发送 multipart 请求并设置准确的 Content-Length。普通字段的编码
// 与 go-telegram/bot 一致：字符串原样写入，其余写 JSON 文本。
func (a *App) uploadMultipart(ctx context.Context, method string, params any, files []uploadFile, dest any, progress uploadProgress) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		v := values[k]
		if string(v) == "null" {
			continue
		}
		text := string(v)
		if len(v) > 0 && v[0] == '"' {
			if err := json.Unmarshal(v, &text); err != nil {
				return err
			}
		}
		if err := mw.WriteField(k, text); err != nil {
			return err
		}
	}

	var parts []io.Reader
	var size int64
	take := func() {
		seg := bytes.Clone(buf.Bytes())
		buf.Reset()
		parts = append(parts, bytes.NewReader(seg))
		size += int64(len(seg))
	}
	for _, f := range files {
		file, err := os.Open(f.path)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if _, err := mw.CreateFormFile(f.field, filepath.Base(f.path)); err != nil {
			return err
		}
		take()
		parts = append(parts, file)
		size += info.Size()
	}
	if err := mw.Close(); err != nil {
		return err
	}
	take()

	token := a.cfg.Bot.Token
	endpoint := strings.TrimSuffix(a.cfg.Bot.APIURL, "/") + "/bot" + token + "/" + method
	var body io.Reader = io.MultiReader(parts...)
	if progress != nil {
		body = &progressReader{r: body, total: size, progress: progress}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("error create request for method %s: %w", method, redactToken(err, token))
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := a.uploadClient.Do(req)
	if err != nil {
		return fmt.Errorf("error do request for method %s, %w", method, redactToken(err, token))
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("error read response body for method %s, %w", method, err)
	}
	return decodeAPIResponse(method, respBody, dest)
}

type progressReader struct {
	r        io.Reader
	sent     int64
	total    int64
	progress uploadProgress
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.sent += int64(n)
		p.progress(p.sent, p.total)
	}
	return n, err
}

// decodeAPIResponse 解析 Bot API 响应，错误与 go-telegram/bot 使用相同的错误类型。
func decodeAPIResponse(method string, body []byte, dest any) error {
	var r struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
		ErrorCode   int             `json:"error_code"`
		Parameters  struct {
			RetryAfter      int   `json:"retry_after"`
			MigrateToChatID int64 `json:"migrate_to_chat_id"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("error decode response body for method %s, %s, %w", method, body, err)
	}
	if !r.OK {
		switch r.ErrorCode {
		case http.StatusBadRequest:
			if r.Parameters.MigrateToChatID != 0 {
				return &tgb.MigrateError{
					Message:         fmt.Sprintf("%s: %s", tgb.ErrorBadRequest, r.Description),
					MigrateToChatID: int(r.Parameters.MigrateToChatID),
				}
			}
			return fmt.Errorf("%w, %s", tgb.ErrorBadRequest, r.Description)
		case http.StatusUnauthorized:
			return fmt.Errorf("%w, %s", tgb.ErrorUnauthorized, r.Description)
		case http.StatusForbidden:
			return fmt.Errorf("%w, %s", tgb.ErrorForbidden, r.Description)
		case http.StatusNotFound:
			return fmt.Errorf("%w, %s", tgb.ErrorNotFound, r.Description)
		case http.StatusConflict:
			return fmt.Errorf("%w, %s", tgb.ErrorConflict, r.Description)
		case http.StatusTooManyRequests:
			return &tgb.TooManyRequestsError{
				Message:    fmt.Sprintf("%s, %s", tgb.ErrorTooManyRequests, r.Description),
				RetryAfter: r.Parameters.RetryAfter,
			}
		default:
			return fmt.Errorf("error response from telegram for method %s, %d %s", method, r.ErrorCode, r.Description)
		}
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(r.Result, dest); err != nil {
		return fmt.Errorf("error decode response result for method %s, %w", method, err)
	}
	return nil
}

// redactToken 隐去错误中请求地址里的 bot token。
func redactToken(err error, token string) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		ue.URL = strings.ReplaceAll(ue.URL, token, "***")
		return ue
	}
	return err
}
