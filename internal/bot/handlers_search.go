package bot

import (
	"context"
	"hash/fnv"
	"math"
	"strconv"
	"strings"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/textx"
)

const (
	searchPageSize      = 8
	searchMaxPage       = 5
	searchLifetime      = 10 * time.Minute
	searchMusicCooldown = 10 * time.Second
	searchPageCooldown  = 5 * time.Second
)

func (a *App) handleSearch(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	args := commandArguments(m)
	if args == "" {
		if _, err := a.reply(ctx, m, render.InputKeyword); err != nil {
			a.log.Errorln(err)
		}
		return
	}
	msgResult, err := a.reply(ctx, m, render.Searching)
	if err != nil {
		a.log.Errorln(err)
		return
	}
	providerName, keyword := a.parseProviderArgs(args)
	text, keyboard, err := a.searchPage(ctx, providerName, keyword, 1)
	if err != nil {
		a.log.Errorln(err)
	}
	if text == "" {
		if err := a.editMessage(ctx, m.Chat.ID, msgResult.ID, render.NoResults, nil); err != nil {
			a.log.Errorln(err)
		}
		return
	}
	if err := a.editMessage(ctx, m.Chat.ID, msgResult.ID, text, keyboard); err != nil {
		a.log.Errorln(err)
	}
}

func (a *App) handleCallbackSearch(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	query := update.CallbackQuery
	msg, answer := a.checkSearch(query, "page", searchPageCooldown)
	if msg != nil {
		answer = a.turnPage(ctx, msg, query.Data)
	}
	a.answerCallback(ctx, query.ID, answer)
}

// turnPage 关键词取自被回复的 /search 命令，回调中的哈希用于确认命令未被编辑
func (a *App) turnPage(ctx context.Context, msg *models.Message, data string) string {
	args := strings.Fields(data)
	if len(args) != 3 || len(msg.ReplyToMessage.Entities) == 0 {
		return render.SearchExpired
	}
	page, _ := strconv.Atoi(args[1])
	query := commandArguments(msg.ReplyToMessage)
	providerName, keyword := a.parseProviderArgs(query)
	if page < 1 || page > searchMaxPage || keyword == "" || searchHash(searchKey(providerName, keyword)) != args[2] {
		return render.SearchExpired
	}
	text, keyboard, err := a.searchPage(ctx, providerName, keyword, page)
	if err != nil {
		a.log.Errorln(err)
	}
	if text == "" {
		return render.NoMoreResults
	}
	if err := a.editMessage(ctx, msg.Chat.ID, msg.ID, text, keyboard); err != nil {
		a.log.Errorln(err)
	}
	return ""
}

// checkSearch 搜索结果按钮仅限发起人在有效期内操作，失败时返回提示
func (a *App) checkSearch(query *models.CallbackQuery, op string, cooldown time.Duration) (*models.Message, string) {
	msg := query.Message.Message
	if msg == nil || msg.ReplyToMessage == nil || msg.ReplyToMessage.From == nil || time.Since(time.Unix(int64(msg.Date), 0)) > searchLifetime {
		return nil, render.SearchExpired
	}
	if query.From.ID != msg.ReplyToMessage.From.ID {
		return nil, render.SearchNotOwner
	}
	if remain := a.cooldown(op, query.From.ID, cooldown); remain > 0 {
		return nil, render.Cooldown(int(math.Ceil(remain.Seconds())))
	}
	return msg, ""
}

type cooldownKey struct {
	op     string
	userID int64
}

// cooldown 冷却中返回剩余时间，否则开始新的冷却
func (a *App) cooldown(op string, userID int64, d time.Duration) time.Duration {
	a.cooldownMu.Lock()
	defer a.cooldownMu.Unlock()
	now := time.Now()
	key := cooldownKey{op, userID}
	if until := a.cooldowns[key]; now.Before(until) {
		return until.Sub(now)
	}
	if a.cooldowns == nil {
		a.cooldowns = make(map[cooldownKey]time.Time)
	}
	// 冷却很短，记录较多时顺带清理
	if len(a.cooldowns) >= 1024 {
		for k, until := range a.cooldowns {
			if !now.Before(until) {
				delete(a.cooldowns, k)
			}
		}
	}
	a.cooldowns[key] = now.Add(d)
	return 0
}

func (a *App) answerCallback(ctx context.Context, queryID, text string) {
	if _, err := a.tg.AnswerCallbackQuery(ctx, &tgb.AnswerCallbackQueryParams{CallbackQueryID: queryID, Text: text}); err != nil {
		a.log.Errorln(err)
	}
}

// parseProviderArgs 首 token 是已启用的 provider 名/code 且后面还有内容时，指定该 provider。
func (a *App) parseProviderArgs(args string) (providerName, keyword string) {
	fields := strings.Fields(args)
	if len(fields) >= 2 {
		if p, ok := a.providers.Lookup(fields[0]); ok {
			return p.Name(), strings.Join(fields[1:], " ")
		}
	}
	return "", args
}

func searchKey(providerName, keyword string) string {
	if providerName == "" {
		return keyword
	}
	return providerName + " " + keyword
}

// searchPage 没有结果时返回空文本
func (a *App) searchPage(ctx context.Context, providerName, keyword string, page int) (string, models.InlineKeyboardMarkup, error) {
	items, count, err := a.providers.Search(ctx, providerName, keyword, searchPageSize, (page-1)*searchPageSize)
	if err != nil || len(items) == 0 {
		return "", models.InlineKeyboardMarkup{}, err
	}
	total := max(page, min(searchMaxPage, (count+searchPageSize-1)/searchPageSize))
	start := (page-1)*searchPageSize + 1
	var text strings.Builder
	text.WriteString(render.SearchPage(page, total))
	refs := make([]provider.Ref, 0, len(items))
	for i, item := range items {
		refs = append(refs, item.Ref)
		label := ""
		if item.Ref.Provider != a.providers.DefaultName() {
			label = a.providerDisplayName(item.Ref)
		}
		text.WriteString(render.SearchItemProvider(start+i, item.Title, textx.JoinNames(item.Artists), label))
	}
	return text.String(), a.searchKeyboard(refs, start, page, page < total, searchHash(searchKey(providerName, keyword))), nil
}

func searchHash(keyword string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(keyword))
	return strconv.FormatUint(uint64(h.Sum32()), 36)
}

func (a *App) handleCallbackMusic(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	query := update.CallbackQuery
	args := strings.Fields(query.Data)
	if len(args) < 2 {
		// 应答回调，否则客户端按钮会一直转圈
		a.answerCallback(ctx, query.ID, render.SearchExpired)
		return
	}
	ref, ok := a.parseRefArg(args[1])
	if !ok {
		a.answerCallback(ctx, query.ID, render.SearchExpired)
		return
	}

	msg, answer := a.checkSearch(query, "music", searchMusicCooldown)
	if msg == nil {
		a.answerCallback(ctx, query.ID, answer)
		return
	}
	a.answerCallback(ctx, query.ID, render.CallbackText)
	if err := a.processMusic(ctx, ref, msg.ReplyToMessage, nil); err != nil {
		a.log.Errorln(err)
	}
}
