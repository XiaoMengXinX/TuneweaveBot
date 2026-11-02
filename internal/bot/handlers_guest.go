package bot

import (
	"context"
	"errors"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/textx"
)

func (a *App) handleGuest(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.GuestMessage
	if m == nil || m.GuestQueryID == "" {
		return
	}
	// 通过 inline 发送的消息也会触发访客模式
	if m.ViaBot != nil && m.ViaBot.ID == a.me.ID {
		return
	}

	var result models.InlineQueryResult
	if reply := m.ReplyToMessage; reply != nil && reply.Voice != nil {
		result = a.guestRecognize(ctx, reply.Voice)
	} else {
		result = a.guestMusic(ctx, m)
	}
	if _, err := tg.AnswerGuestQuery(ctx, &tgb.AnswerGuestQueryParams{
		GuestQueryID: m.GuestQueryID,
		Result:       result,
	}); err != nil {
		a.log.Errorln(err)
	}
}

// guestMusic 访客模式无法上传文件，未缓存的歌曲须先上传到缓存群
func (a *App) guestMusic(ctx context.Context, m *models.Message) models.InlineQueryResult {
	ref, ok := a.guestRef(ctx, m)
	if !ok {
		return guestArticle("使用帮助", render.GuestUsage, nil)
	}
	// 缓存键是规范歌曲，节目等子资源先归一
	canonical := *ref
	if ref.Sub != "" {
		resolved, err := a.providers.Canonicalize(ctx, *ref)
		if err != nil {
			a.log.Errorln(err)
			return guestArticle(a.providerDisplayName(*ref), render.FetchInfoFailed, nil)
		}
		canonical = resolved
	}

	songInfo, cached := a.store.GetByRef(canonical.Provider, canonical.ID)
	if chatID := a.cacheChat.Load(); !cached && chatID != 0 && a.auth.Available(canonical.Provider) {
		var err error
		// display 也用规范歌曲，让发送的按钮与缓存链接一致（均指向规范歌曲）
		if songInfo, err = a.cacheMusic(ctx, canonical, canonical, nil, m, chatID); err != nil {
			a.log.Errorln(err)
		}
	}
	if songInfo.FileID != "" {
		return a.cachedMusicResult("music", songInfo, nil)
	}

	text := render.GuestNoCache
	if songInfo.SongName != "" {
		text = render.GuestMusicResult(songInfo.SongName, songInfo.SongArtists, songInfo.SongAlbum)
	} else {
		// 只取展示元数据，避免为未缓存歌曲解析下载地址
		summary, err := a.providers.Metadata(ctx, canonical)
		if err != nil {
			a.log.Errorln(err)
		} else if summary != nil {
			text = render.GuestMusicResult(summary.Title, textx.JoinNames(summary.Artists), summary.Album)
		}
	}
	name := a.providerDisplayName(canonical)
	return guestArticle(name, text, a.guestKeyboard(name, a.refLink(canonical), a.startParam(canonical)))
}

func (a *App) guestRef(ctx context.Context, m *models.Message) (*provider.Ref, bool) {
	if ref, ok := a.messageRef(ctx, m); ok {
		return ref, true
	}
	if m.ReplyToMessage != nil {
		return a.messageRef(ctx, m.ReplyToMessage)
	}
	return nil, false
}

func (a *App) cacheMusic(ctx context.Context, ref, display provider.Ref, track *provider.Track, m *models.Message, chatID int64) (store.SongInfo, error) {
	if err := a.musicSem.Acquire(ctx, 1); err != nil {
		return store.SongInfo{}, err
	}
	defer a.musicSem.Release(1)
	if songInfo, ok := a.store.GetByRef(ref.Provider, ref.ID); ok && songInfo.FileID != "" {
		return songInfo, nil
	}
	return a.fetchMusic(ctx, ref, display, track, m, nil, chatID, nil, func(string) error { return nil })
}

func (a *App) guestRecognize(ctx context.Context, voice *models.Voice) models.InlineQueryResult {
	audio, err := a.readVoice(ctx, voice)
	if err != nil {
		a.log.Errorln(err)
		return guestArticle("音乐识别", render.RecognizeReadFailed, nil)
	}
	ref, noResult, err := a.recognize(ctx, audio)
	if ref == nil {
		if noResult && err == nil {
			return guestArticle("音乐识别", render.RecognizeNoResult, nil)
		}
		if err != nil {
			a.log.Errorln(err)
		}
		return guestArticle("音乐识别", render.RecognizeFailed, nil)
	}

	text := render.RecognizeSuccess
	if info, ok := a.store.GetByRef(ref.Provider, ref.ID); ok {
		text = render.RecognizeResult(info.SongName, info.SongArtists, info.SongAlbum)
	} else if summary, err := a.providers.Metadata(ctx, *ref); err != nil {
		// 空结果不算错误，保持识别成功文案
		if !errors.Is(err, provider.ErrNotFound) {
			a.log.Errorln(err)
		}
	} else if summary != nil {
		text = render.RecognizeResult(summary.Title, textx.JoinNames(summary.Artists), summary.Album)
	}
	name := a.providerDisplayName(*ref)
	return guestArticle(name, text, a.guestKeyboard(name, a.refLink(*ref), a.startParam(*ref)))
}

func guestArticle(title, text string, markup models.ReplyMarkup) *models.InlineQueryResultArticle {
	disabled := true
	return &models.InlineQueryResultArticle{
		ID:    "guest",
		Title: title,
		InputMessageContent: &models.InputTextMessageContent{
			MessageText:        text,
			ParseMode:          models.ParseModeHTML,
			LinkPreviewOptions: &models.LinkPreviewOptions{IsDisabled: &disabled},
		},
		ReplyMarkup: markup,
	}
}
