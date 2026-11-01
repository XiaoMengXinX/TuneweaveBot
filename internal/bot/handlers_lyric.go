package bot

import (
	"context"
	"strings"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/textx"
)

func (a *App) handleLyric(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	source := m
	args := commandArguments(m)
	if args == "" {
		if m.ReplyToMessage == nil {
			if _, err := a.reply(ctx, m, render.InputContent); err != nil {
				a.log.Errorln(err)
			}
			return
		}
		source = m.ReplyToMessage
	}

	msgResult, err := a.reply(ctx, source, render.FetchingLyric)
	if err != nil {
		a.log.Errorln(err)
		return
	}
	sendFailed := func() {
		if err := a.editMessage(ctx, msgResult.Chat.ID, msgResult.ID, render.GetLrcFailed, nil); err != nil {
			a.log.Errorln(err)
		}
	}

	var ref *provider.Ref
	if source != m {
		// 回复消息取歌词：只认链接，找不到就提示，不拿空关键词去搜索
		if matched, ok := a.messageRef(ctx, source); ok {
			ref = matched
		} else {
			if err := a.editMessage(ctx, msgResult.Chat.ID, msgResult.ID, render.NoResults, nil); err != nil {
				a.log.Errorln(err)
			}
			return
		}
	} else if matched, _, ok := a.providers.Match(ctx, args); ok {
		ref = matched
	}
	if ref == nil {
		items, _, err := a.providers.Search(ctx, "", args, 5, 0)
		if err != nil {
			a.log.Errorln(err)
		}
		if len(items) == 0 {
			if err := a.editMessage(ctx, msgResult.Chat.ID, msgResult.ID, render.NoResults, nil); err != nil {
				a.log.Errorln(err)
			}
			return
		}
		ref = &items[0].Ref
	}
	if ref.Sub != "" {
		if err := a.editMessage(ctx, msgResult.Chat.ID, msgResult.ID, render.LrcNoProgram, nil); err != nil {
			a.log.Errorln(err)
		}
		return
	}

	lyric, err := a.providers.Lyrics(ctx, *ref)
	if err != nil {
		a.log.Errorln(err)
		sendFailed()
		return
	}
	if lyric.LRC == "" {
		sendFailed()
		return
	}

	// 歌词直接从内存上传，避免落盘引起的并发同名文件互相截断
	fileName := songFileName(textx.JoinNames(lyric.Artists), lyric.Title, "lrc")
	params := &tgb.SendDocumentParams{
		ChatID:          source.Chat.ID,
		Document:        &models.InputFileUpload{Filename: fileName, Data: strings.NewReader(lyric.LRC)},
		ReplyParameters: &models.ReplyParameters{MessageID: source.ID},
	}
	if text := textx.LrcText(lyric.LRC); text != "" {
		params.Caption = render.LyricCaption(text)
		params.ParseMode = models.ParseModeHTML
	}
	if _, err := a.tg.SendDocument(ctx, params); err != nil {
		a.log.Errorln(err)
		return
	}
	if err := a.deleteMessage(ctx, msgResult.Chat.ID, msgResult.ID); err != nil {
		a.log.Errorln(err)
	}
}
