package bot

import (
	"fmt"

	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

// track 非 nil 时使用本次解析的页面链接（如节目页）；分享按钮始终使用规范歌曲链接。
func musicKeyboard(songInfo store.SongInfo, track *provider.Track) models.InlineKeyboardMarkup {
	// 打开按钮用分享链接，分享按钮用查询链接
	share := songInfo.ShareText
	if share == "" {
		share = songInfo.PageURL
	}
	link := share
	if track != nil {
		if track.ShareText != "" {
			share = track.ShareText
			link = share
		}
		// 节目等子资源的页面链接与缓存里的规范歌曲页不同，仅此时覆盖打开按钮
		if track.PageURL != "" && track.PageURL != songInfo.PageURL {
			link = track.PageURL
		}
	}
	return models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{
				Text: fmt.Sprintf("%s- %s", songInfo.SongName, songInfo.SongArtists),
				URL:  link,
			}},
			{{
				Text:              render.SendMeTo,
				SwitchInlineQuery: &share,
			}},
		},
	}
}

func linkKeyboard(providerName, link string) models.InlineKeyboardMarkup {
	return models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{{
		Text: fmt.Sprintf("在%s中打开", providerName),
		URL:  link,
	}}}}
}

func (a *App) guestKeyboard(providerName, link, startParam string) models.InlineKeyboardMarkup {
	keyboard := linkKeyboard(providerName, link)
	keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []models.InlineKeyboardButton{{
		Text: "在 Bot 中获取歌曲",
		URL:  fmt.Sprintf("https://t.me/%s?start=%s", a.me.Username, startParam),
	}})
	return keyboard
}

func (a *App) searchKeyboard(refs []provider.Ref, start, page int, hasMore bool, hash string) models.InlineKeyboardMarkup {
	buttons := make([]models.InlineKeyboardButton, 0, len(refs))
	for i, ref := range refs {
		buttons = append(buttons, models.InlineKeyboardButton{
			Text:         fmt.Sprintf("%d", start+i),
			CallbackData: a.callbackRef(ref),
		})
	}
	keyboard := [][]models.InlineKeyboardButton{buttons}
	var pages []models.InlineKeyboardButton
	if page > 1 {
		pages = append(pages, models.InlineKeyboardButton{Text: "⬅️ 上一页", CallbackData: fmt.Sprintf("search %d %s", page-1, hash)})
	}
	if hasMore {
		pages = append(pages, models.InlineKeyboardButton{Text: "下一页 ➡️", CallbackData: fmt.Sprintf("search %d %s", page+1, hash)})
	}
	if len(pages) != 0 {
		keyboard = append(keyboard, pages)
	}
	return models.InlineKeyboardMarkup{InlineKeyboard: keyboard}
}
