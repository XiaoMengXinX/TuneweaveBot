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

// sendAudioFile 上传本地音频（及缩略图）。go-telegram/bot 会把整个请求体读进内存，
// 上 GB 的无损文件会占用约两倍于文件的内存；这里只把表单字段放在内存里，
// 文件内容在发送时从磁盘读取。
func (a *App) sendAudioFile(ctx context.Context, params *tgb.SendAudioParams, audioPath, thumbPath string) (*models.Message, error) {
	fields := *params
	fields.Audio, fields.Thumbnail = nil, nil
	files := []uploadFile{{field: "audio", path: audioPath}}
	if thumbPath != "" {
		files = append(files, uploadFile{field: "thumbnail", path: thumbPath})
	}
	var msg models.Message
	if err := a.uploadMultipart(ctx, "sendAudio", &fields, files, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// uploadMultipart 以 multipart 流式发送请求：先算出各部分长度并设置 Content-Length，
// 文件内容不经内存缓冲。普通字段按 JSON 编码后写入：字符串原样，其余为 JSON 文本，
// 与 go-telegram/bot 的表单编码一致。
func (a *App) uploadMultipart(ctx context.Context, method string, params any, files []uploadFile, dest any) error {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, io.MultiReader(parts...))
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("error read response body for method %s, %w", method, err)
	}
	return decodeAPIResponse(method, body, dest)
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
