package bot

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
)

// webText 是面板接口返回给用户的文案，按前端的 X-Language 请求头选择语言。
type webText struct{ zh, en string }

// requestLang 返回请求的界面语言：en 或 zh（默认）。
func requestLang(r *http.Request) string {
	if strings.HasPrefix(r.Header.Get("X-Language"), "en") {
		return "en"
	}
	return "zh"
}

func (t webText) in(r *http.Request) string {
	if requestLang(r) == "en" {
		return t.en
	}
	return t.zh
}

func writeError(w http.ResponseWriter, r *http.Request, status int, text webText) {
	writeJSON(w, status, map[string]string{"error": text.in(r)})
}

var (
	msgNotFoundAPI      = webText{"接口不存在", "Not found"}
	msgInternal         = webText{"服务器内部错误", "Internal server error"}
	msgNotInTelegram    = webText{"请在 Telegram 中打开", "Please open this in Telegram"}
	msgAuthFailed       = webText{"身份校验失败，请重新打开面板", "Authentication failed, please reopen the panel"}
	msgAdminOnly        = webText{"需要管理员权限", "Administrator access required"}
	msgTooManyRequests  = webText{"请求过于频繁，请稍后再试", "Too many requests, please try again later"}
	msgBadRequest       = webText{"请求格式错误", "Malformed request"}
	msgUnknownProvider  = webText{"未知的平台", "Unknown platform"}
	msgKeywordTooLong   = webText{"关键词太长", "Keyword is too long"}
	msgInputKeyword     = webText{render.InputKeyword, "Please enter a keyword"}
	msgNoMoreResults    = webText{render.NoMoreResults, "No more results"}
	msgSearchBusy       = webText{"搜索繁忙，请稍后再试", "Search is busy, please try again later"}
	msgSearchFailed     = webText{"搜索失败，请稍后重试", "Search failed, please try again later"}
	msgSongNotFound     = webText{"歌曲不存在", "Song not found"}
	msgMusicBusy        = webText{"上一首歌还在处理中，完成后再点下一首", "Your previous song is still being processed"}
	msgStatsDays        = webText{"days 须为 7、30 或 90", "days must be 7, 30 or 90"}
	msgNoCache          = webText{render.NoCache, "Song is not cached"}
	msgPluginNotFound   = webText{"插件不存在", "Plugin not found"}
	msgNoPluginSelected = webText{"未指定插件", "No plugin selected"}
	msgUpdateBusy       = webText{render.UpdateBusy, "Another update is in progress"}
	msgCacheChatFailed  = webText{render.CacheChatUnreachable, "Cannot send messages to that chat; make sure the bot has joined it"}
	msgCacheChatPersist = webText{"配置文件写入失败，本次设置已生效，重启后将失效", "Saved for now, but the config file could not be written; the setting will be lost on restart"}
	msgBadDate          = webText{"日期格式错误", "Invalid date"}
	msgLoginUnsupported = webText{render.LoginUnsupported, "This platform does not support login"}
	msgLoginBusy        = webText{render.LoginBusy, "Another login session is in progress"}
	msgLoginBeginFailed = webText{render.LoginKeyFailed, "Failed to start login, please try again later"}
	msgLoginNoSession   = webText{"当前没有进行中的登录会话", "No login session in progress"}
	msgTokenEmpty       = webText{"请输入 token", "Please enter a token"}
	msgTokenSubmitted   = webText{"token 已提交，正在校验", "Token submitted, verifying"}
)

func msgCooldown(seconds int) webText {
	return webText{render.Cooldown(seconds), fmt.Sprintf("Too many requests, please try again in %d s", seconds)}
}
