package bot

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/textx"
)

func (a *App) handleInline(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	query := update.InlineQuery
	var err error
	text := strings.ToLower(strings.TrimSpace(query.Query))
	switch {
	case text == "help":
		err = a.answerInlineHelp(ctx, query)
	case strings.HasPrefix(text, "search") && (len(text) == len("search") || text[len("search")] == ' '):
		err = a.answerInlineSearch(ctx, query)
	default:
		if ref, _, ok := a.providers.Match(ctx, query.Query); ok {
			err = a.answerInlineMusic(ctx, query, *ref)
		} else {
			err = a.answerInlineEmpty(ctx, query)
		}
	}
	if err != nil {
		a.log.Errorln(err)
	}
}

func (a *App) answerInline(ctx context.Context, queryID string, results []models.InlineQueryResult, cacheTime int, button *models.InlineQueryResultsButton) error {
	_, err := a.tg.AnswerInlineQuery(ctx, &tgb.AnswerInlineQueryParams{
		InlineQueryID: queryID,
		Results:       results,
		CacheTime:     cacheTime,
		Button:        button,
	})
	return err
}

func (a *App) answerInlineMusic(ctx context.Context, query *models.InlineQuery, ref provider.Ref) error {
	id := inlineID(query.ID)
	// 缓存键是规范歌曲，节目等子资源须先归一，否则查不到缓存或命中错误歌曲
	lookup := ref
	if ref.Sub != "" {
		if canonical, err := a.providers.Canonicalize(ctx, ref); err == nil {
			lookup = canonical
		}
	}
	if songInfo, ok := a.store.GetByRef(lookup.Provider, lookup.ID); ok && songInfo.FileID != "" && songInfo.SongName != "" {
		return a.answerInline(ctx, query.ID, []models.InlineQueryResult{a.cachedMusicResult(id, songInfo, nil)}, 3600, nil)
	}
	return a.answerInline(ctx, query.ID, []models.InlineQueryResult{
		&models.InlineQueryResultArticle{
			ID:                  id,
			Title:               render.NoCache,
			Description:         render.TapToDownload,
			InputMessageContent: &models.InputTextMessageContent{MessageText: query.Query},
		},
	}, 60, &models.InlineQueryResultsButton{
		Text:           render.TapMeToDown,
		StartParameter: a.startParam(ref),
	})
}

func (a *App) answerInlineHelp(ctx context.Context, query *models.InlineQuery) error {
	randomID := time.Now().UnixMicro()
	return a.answerInline(ctx, query.ID, []models.InlineQueryResult{
		&models.InlineQueryResultArticle{
			ID:                  fmt.Sprintf("%d", randomID),
			Title:               render.InlineHelpStep1,
			InputMessageContent: &models.InputTextMessageContent{MessageText: render.BotName},
		},
		&models.InlineQueryResultArticle{
			ID:                  fmt.Sprintf("%d", randomID+1),
			Title:               render.InlineHelpStep2,
			InputMessageContent: &models.InputTextMessageContent{MessageText: render.BotName},
		},
	}, 3600, nil)
}

func (a *App) answerInlineSearch(ctx context.Context, query *models.InlineQuery) error {
	randomID := time.Now().UnixMicro()
	// 分发时按 trim 后的文本匹配，这里也须从 trim 后的文本切片，避免前导空格错位
	keyword := strings.TrimSpace(strings.TrimSpace(query.Query)[len("search"):])
	if keyword == "" {
		return a.answerInline(ctx, query.ID, []models.InlineQueryResult{
			&models.InlineQueryResultArticle{
				ID:                  fmt.Sprintf("%d", randomID),
				Title:               render.InlineInputKeyword,
				InputMessageContent: &models.InputTextMessageContent{MessageText: render.BotName},
			},
		}, 3600, nil)
	}
	const inlineSearchLimit = 10
	items, _, err := a.providers.Search(ctx, "", keyword, inlineSearchLimit, 0)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return a.answerInline(ctx, query.ID, []models.InlineQueryResult{
			&models.InlineQueryResultArticle{
				ID:                  fmt.Sprintf("%d", randomID),
				Title:               render.NoResults,
				InputMessageContent: &models.InputTextMessageContent{MessageText: render.NoResults},
			},
		}, 3600, nil)
	}
	var results []models.InlineQueryResult
	for i := range items {
		item := items[i]
		results = append(results, &models.InlineQueryResultArticle{
			ID:                  fmt.Sprintf("%d", randomID+int64(i)),
			Title:               item.Title,
			Description:         textx.JoinNames(item.Artists),
			InputMessageContent: &models.InputTextMessageContent{MessageText: a.musicCommand(item.Ref)},
		})
	}
	return a.answerInline(ctx, query.ID, results, 3600, nil)
}

func (a *App) answerInlineEmpty(ctx context.Context, query *models.InlineQuery) error {
	return a.answerInline(ctx, query.ID, []models.InlineQueryResult{
		&models.InlineQueryResultArticle{
			ID:                  inlineID(query.ID),
			Title:               render.InlineHelpHint,
			InputMessageContent: &models.InputTextMessageContent{MessageText: render.BotName},
		},
	}, 3600, nil)
}

// musicCommand 生成点击 inline 结果后发送的消息文本。
func (a *App) musicCommand(ref provider.Ref) string {
	if ref.Provider == a.providers.DefaultName() && ref.Sub == "" && isDigits(ref.ID) {
		return "/music " + ref.ID
	}
	return "/music " + a.encodeRef(ref)
}

// inlineID 保证 1-64 字节。
func inlineID(seed string) string {
	if len(seed) <= 64 {
		return seed
	}
	sum := sha256.Sum256([]byte(seed))
	return "h" + base64.RawURLEncoding.EncodeToString(sum[:])
}
