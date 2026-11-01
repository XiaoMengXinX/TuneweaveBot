package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

func (a *App) handleRecognize(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	if m.ReplyToMessage == nil {
		if _, err := a.reply(ctx, m, render.ReplyVoiceFirst); err != nil {
			a.log.Errorln(err)
		}
		return
	}
	voice := m.ReplyToMessage
	if voice.Voice == nil {
		if _, err := a.reply(ctx, voice, render.ReplyVoiceFirst); err != nil {
			a.log.Errorln(err)
		}
		return
	}

	status, err := a.reply(ctx, voice, render.Recognizing)
	if err != nil {
		a.log.Errorln(err)
		return
	}
	setStatus := func(text string) {
		if err := a.editMessage(ctx, voice.Chat.ID, status.ID, text, nil); err != nil {
			a.log.Errorln(err)
		}
	}

	audio, err := a.readVoice(ctx, voice.Voice)
	if err != nil {
		a.log.Errorln(err)
		setStatus(render.RecognizeReadFailed)
		return
	}
	ref, noResult, err := a.recognize(ctx, audio)
	if ref == nil {
		if noResult && err == nil {
			setStatus(render.RecognizeNoResult)
		} else {
			if err != nil {
				a.log.Errorln(err)
			}
			setStatus(render.RecognizeFailed)
		}
		return
	}

	keyboard := linkKeyboard(a.providerDisplayName(*ref), a.refLink(*ref))
	showResult := func(text string) {
		if err := a.editMessage(ctx, voice.Chat.ID, status.ID, text, keyboard); err != nil {
			a.log.Errorln(err)
		}
	}
	shown := false
	if err := a.processMusic(ctx, *ref, voice, func(info store.SongInfo) {
		showResult(render.RecognizeResult(info.SongName, info.SongArtists, info.SongAlbum))
		shown = true
	}); err != nil {
		a.log.Errorln(err)
	}
	if !shown {
		showResult(render.RecognizeSuccess)
	}
}

// recognize 依次尝试已启用的 recognizer；全部无结果时返回 noResult。
func (a *App) recognize(ctx context.Context, audio []byte) (*provider.Ref, bool, error) {
	if len(a.recognizers) == 0 {
		return nil, false, fmt.Errorf("未启用任何识曲 recognizer")
	}
	noResult := false
	var lastErr error
	for _, r := range a.recognizers {
		ref, err := r.Recognize(ctx, audio, "audio/ogg")
		if err == nil {
			return ref, false, nil
		}
		if errors.Is(err, provider.ErrNoResult) {
			noResult = true
			continue
		}
		lastErr = err
	}
	return nil, noResult, lastErr
}

// maxVoiceBytes 限制识曲读取的语音大小，避免超大文件占满内存
const maxVoiceBytes = 20 << 20

func (a *App) readVoice(ctx context.Context, voice *models.Voice) ([]byte, error) {
	file, err := a.tg.GetFile(ctx, &tgb.GetFileParams{FileID: voice.FileID})
	if err != nil {
		return nil, err
	}
	// 识曲只需内存中的字节，无需落盘
	source, cleanup, err := a.openVoice(ctx, file)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	data, err := io.ReadAll(io.LimitReader(source, maxVoiceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxVoiceBytes {
		return nil, fmt.Errorf("语音文件过大（超过 %d 字节）", maxVoiceBytes)
	}
	return data, nil
}

func (a *App) openVoice(ctx context.Context, file *models.File) (io.Reader, func(), error) {
	if a.cfg.Bot.LocalMode {
		if !filepath.IsAbs(file.FilePath) {
			return nil, nil, fmt.Errorf("本地模式需要 Bot API 返回绝对文件路径，请确认服务已启用 --local")
		}
		localFile, err := os.Open(file.FilePath)
		if err != nil {
			return nil, nil, fmt.Errorf("读取 Bot API 本地文件失败，请检查共享目录和读取权限: %w", err)
		}
		info, err := localFile.Stat()
		if err != nil {
			_ = localFile.Close()
			return nil, nil, err
		}
		if !info.Mode().IsRegular() {
			_ = localFile.Close()
			return nil, nil, fmt.Errorf("Bot API 本地路径不是普通文件")
		}
		return localFile, func() { _ = localFile.Close() }, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.tg.FileDownloadLink(file), nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, nil, fmt.Errorf("下载语音失败: %s", resp.Status)
	}
	return resp.Body, func() { _ = resp.Body.Close() }, nil
}
