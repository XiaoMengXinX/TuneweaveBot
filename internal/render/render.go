// Package render 界面文案，统一 HTML parse mode，动态内容经转义
package render

import (
	"fmt"
	"html"
	"strings"
	"unicode/utf16"
)

const (
	InputKeyword     = "请输入搜索关键词"
	InputIDorKeyword = "请输入歌曲ID或歌曲关键词"
	InputIDorLink    = "请输入歌曲ID或歌曲分享链接"
	InputContent     = "请输入歌曲关键词/歌曲分享链接/歌曲ID"
	Searching        = "搜索中..."
	NoResults        = "未找到结果"
	NoCache          = "歌曲未缓存"
	TapToDownload    = "点击上方按钮缓存歌曲"
	TapMeToDown      = "点我缓存歌曲"
	HitCache         = "命中缓存, 正在发送中..."
	SendMeTo         = "Send me to..."
	GetLrcFailed     = "获取歌词失败, 歌曲可能不存在或为纯音乐"
	LrcNoProgram     = "电台节目不支持获取歌词"
	GetURLFailed     = "获取歌曲下载链接失败"
	FetchInfo        = "正在获取歌曲信息..."
	FetchInfoFailed  = "获取歌曲信息失败"
	WaitForDown      = "等待下载中..."
	Downloading      = "下载中..."
	Redownloading    = "下载失败，尝试重新下载中..."
	Uploading        = "下载完成, 发送中..."
	Md5VerFailed     = "MD5校验失败"
	RetryLater       = "请稍后重试"
	Reloading        = "将在当前任务完成后重新加载配置与插件"
	CallbackText     = "Success"
	FetchingLyric    = "正在获取歌词中"
	DownloadFailed   = "下载失败"
	SendFailed       = "发送失败"
	ProcessFailed    = "文件处理失败"
	SearchExpired    = "搜索已失效，请重新搜索"
	NoMoreResults    = "没有更多结果了"
	SearchNotOwner   = "只有发起搜索的人可以操作"

	ReplyVoiceFirst     = "请先发送一条包含音乐片段的语音，再回复该语音发送 /recognize。"
	GuestUsage          = "回复一条网易云音乐分享链接或包含音乐片段的语音，并在回复中 @我，即可发送或识别歌曲。"
	GuestNoCache        = "歌曲未缓存，请点击下方按钮在 Bot 中获取"
	Recognizing         = "正在识别音乐，请稍候..."
	RecognizeNoResult   = "暂未识别到歌曲\n请尝试录制更清晰、稍长一些的音乐片段，再试一次。"
	RecognizeFailed     = "音乐识别暂时失败，请稍后重试。"
	RecognizeReadFailed = "暂时无法读取这条语音\n请稍后重试，或重新发送语音。"
	RecognizeSuccess    = "已识别到歌曲"
	InlineHelpHint      = "输入 help 获取帮助"
	InlineHelpStep1     = "1.粘贴音乐分享URL或输入MusicID"
	InlineHelpStep2     = "2.输入 search+关键词 搜索歌曲"
	InlineInputKeyword  = "请输入关键词"
	BotName             = "TuneweaveBot"

	LoginBusy        = "已有登录会话进行中"
	LoginKeyFailed   = "发起登录失败，请稍后重试"
	LoginExpired     = "登录已过期，请重新发送 /login"
	LoginCheckFailed = "查询登录状态失败，请稍后重试"
	LoginCancelled   = "已取消登录"
	LoginReloading   = "bot 正在重载，登录已中断，请稍后重新 /login"
	LoginFailed      = "登录失败，请稍后重试"
	CancelLogin      = "取消登录"

	LoginUnsupported = "当前 Provider 不支持登录"
	LoginSessionOver = "登录会话已结束，可发送 /login"
	LoginNoSession   = "当前没有进行中的登录会话，可发送 /login"

	CheckingUpdates    = "正在检查插件更新..."
	NoPluginUpdates    = "插件均已是最新版本"
	NoUpdatablePlugins = "没有可检查更新的插件（插件需在 manifest 中声明 update_url 与 version_url）"
	UpdateNow          = "立即更新"
	UpdateExpired      = "更新按钮已失效，请重新 /checkupdate"
	UpdateBusy         = "已有插件更新在进行中"
	UpdatingPlugins    = "正在下载并校验插件..."

	InputChatID          = "缓存群 ID 须为数字，也可以在目标群内直接发送 /setcachechat"
	CacheChatTest        = "已设为缓存群，Guest Mode 点歌时会先把未缓存的歌曲上传到这里"
	CacheChatUnreachable = "无法向该聊天发送消息，请确认 bot 已加入并可以发言"
	CacheChatDisabled    = "已关闭缓存群"
)

func CookieExpired(provider string) string {
	return Escape(provider) + " 登录已失效，请联系 bot 管理员"
}

// name 为 provider 内部名，用于拼出可直接复制的 /login 命令。
func CookieExpiredAdminWarn(display, name string) string {
	return fmt.Sprintf("%s 登录已失效，新歌下载已暂停，请发送 <code>/login %s</code> 重新登录", Escape(display), Escape(name))
}

func CookieResumedAdmin(provider string) string {
	return Escape(provider) + " 登录已生效，新歌下载已恢复"
}

func LoginQRCaption(remainSec int) string {
	return fmt.Sprintf("请扫描二维码登录\n剩余有效期 %d 秒", remainSec)
}

func LoginScanned(nickname string) string {
	if nickname != "" {
		return "已扫码，等待确认: " + Escape(nickname)
	}
	return "已扫码，等待确认"
}

func LoginOK(nickname string) string {
	if nickname != "" {
		return "登录成功: " + Escape(nickname)
	}
	return "登录成功"
}

// PluginVersion 格式化插件版本，有构建时间时一并显示。
func PluginVersion(version, buildTime string) string {
	if buildTime == "" {
		return "v" + version
	}
	return fmt.Sprintf("v%s (%s)", version, buildTime)
}

// PluginUpdateLine 是更新列表中的一行，from/to 由 PluginVersion 生成。
func PluginUpdateLine(displayName, name, from, to string) string {
	return fmt.Sprintf("• %s (%s): %s → %s", Escape(displayName), Escape(name), Escape(from), Escape(to))
}

// PluginUpdatesAvailable 列出可用更新；checkErr 非空时附上检查失败的插件。
func PluginUpdatesAvailable(lines []string, validFor, checkErr string) string {
	text := "发现插件更新：\n" + strings.Join(lines, "\n")
	if checkErr != "" {
		text += "\n\n部分插件检查失败：\n<code>" + Escape(checkErr) + "</code>"
	}
	return text + "\n\n按钮 " + validFor + " 内有效"
}

func UpdateCheckFailed(err string) string {
	return "检查插件更新失败：\n<code>" + Escape(err) + "</code>"
}

// PluginUpdateResult 汇总更新结果；有插件更新成功时提示将重载。
func PluginUpdateResult(updated, failed []string) string {
	var b strings.Builder
	if len(updated) > 0 {
		b.WriteString("已更新：\n" + strings.Join(updated, "\n"))
	}
	if len(failed) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("未更新：\n" + strings.Join(failed, "\n"))
	}
	if len(updated) > 0 {
		b.WriteString("\n\n将在当前任务完成后重新加载插件")
	}
	return b.String()
}

// PluginRolledBack 通知管理员插件新版本加载失败、已回滚到更新前的版本。
func PluginRolledBack(displayName, name, version, err string) string {
	return fmt.Sprintf("插件 %s (%s) 更新后加载失败，已回滚到 %s\n<code>%s</code>",
		Escape(displayName), Escape(name), Escape(version), Escape(err))
}

// PluginUpdateFailedLine 是未更新列表中的一行。
func PluginUpdateFailedLine(displayName, name, reason string) string {
	return fmt.Sprintf("• %s (%s): %s", Escape(displayName), Escape(name), Escape(reason))
}

func Escape(s string) string { return html.EscapeString(s) }

func MusicStatus(name, album, ext string, sizeMB float64, suffix string) string {
	return fmt.Sprintf("%s\n专辑: %s\n%s %.2fMB\n%s", Escape(name), Escape(album), Escape(ext), sizeMB, suffix)
}

func DownloadStatus(speed string, writtenMB, totalMB float64, percent int64) string {
	return fmt.Sprintf(" %s\n%.2fMB/%.2fMB %d%%", Escape(speed), writtenMB, totalMB, percent)
}

func DownloadingWithInfo(name, album, speed string, writtenMB, totalMB float64, percent int64) string {
	return fmt.Sprintf("%s\n专辑: %s\n%s%s", Escape(name), Escape(album), Downloading,
		DownloadStatus(speed, writtenMB, totalMB, percent))
}

func SongInfo(name, artists, album string) string {
	return fmt.Sprintf("「%s」- %s\n专辑: %s", Escape(name), Escape(artists), Escape(album))
}

func RecognizeResult(name, artists, album string) string {
	return "🎵 识别成功\n" + SongInfo(name, artists, album)
}

func GuestMusicResult(name, artists, album string) string {
	return SongInfo(name, artists, album) + "\n" + GuestNoCache
}

// LyricCaption caption 按 UTF-16 计算长度且不超过 1024
func LyricCaption(lyric string) string {
	const limit = 1024
	if truncated := truncateUTF16(lyric, limit); truncated != lyric {
		lyric = truncateUTF16(lyric, limit-1) + "…"
	}
	return "<blockquote expandable>" + Escape(lyric) + "</blockquote>"
}

func truncateUTF16(s string, limit int) string {
	n := 0
	for i, r := range s {
		if n += utf16.RuneLen(r); n > limit {
			return s[:i]
		}
	}
	return s
}

// DefaultProviderName 是 provider 未提供来源名称时的兜底展示名。
const DefaultProviderName = "网易云音乐"

func MusicCaption(name, artists, album, ext string, sizeMB, kbps float64, providerName, botName string) string {
	if providerName == "" {
		providerName = DefaultProviderName
	}
	// Telegram caption 上限 1024（按 UTF-16 计）。在转义前按原文截断，
	// 避免切断 HTML 实体；给标签与 via 行留出余量。
	const fieldLimit = 280
	name = clampUTF16(name, fieldLimit)
	artists = clampUTF16(artists, fieldLimit)
	album = clampUTF16(album, fieldLimit)
	return fmt.Sprintf("%s\n#%s #%s %.2fMB %.2fkbps\nvia @%s",
		SongInfo(name, artists, album), Escape(providerName), Escape(ext), sizeMB, kbps, Escape(botName))
}

// clampUTF16 按 UTF-16 长度截断原文，超长时追加省略号。
func clampUTF16(s string, limit int) string {
	if truncateUTF16(s, limit) == s {
		return s
	}
	return truncateUTF16(s, limit-1) + "…"
}

func About(runtimeVer, versionName, commitSHA, buildTime, buildArch string) string {
	return fmt.Sprintf(`<b>TuneweaveBot</b>
Github: https://github.com/XiaoMengXinX/TuneweaveBot

[编译环境] %s
[编译版本] %s
[编译哈希] %s
[编译日期] %s
[运行环境] %s`,
		Escape(runtimeVer), Escape(versionName), Escape(commitSHA), Escape(buildTime), Escape(buildArch))
}

func StatusInfo(chatTitle, chatUsername string, chatID, userID, total, chatCount, userCount int64) string {
	var chatInfo string
	switch {
	case chatUsername != "" && chatTitle == "":
		chatInfo = fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, chatID, Escape(chatUsername))
	case chatUsername != "":
		chatInfo = fmt.Sprintf(`<a href="https://t.me/%s">%s</a>`, Escape(chatUsername), Escape(chatTitle))
	default:
		chatInfo = Escape(chatTitle)
	}
	return fmt.Sprintf(`<b>[统计信息]</b>
数据库中总缓存歌曲数量: %d
当前对话 [%s] 缓存歌曲数量: %d
当前用户 [<a href="tg://user?id=%d">%d</a>] 缓存歌曲数量: %d`,
		total, chatInfo, chatCount, userID, userID, userCount)
}

func CacheChatSet(chatID int64) string {
	return fmt.Sprintf("缓存群已设置为 <code>%d</code>", chatID)
}

func RmcacheReport(name string) string {
	return fmt.Sprintf("清除 [%s] 缓存成功", Escape(name))
}

func Cooldown(seconds int) string {
	return fmt.Sprintf("操作太频繁，请 %d 秒后再试", seconds)
}

func SearchPage(page, total int) string {
	return fmt.Sprintf("<code>第[%d/%d]页</code>\n", page, total)
}

func SearchItem(n int, name, artists string) string {
	return fmt.Sprintf("<code>%-3s</code> <code>%s - %s</code>\n", fmt.Sprintf("%d.", n), Escape(name), Escape(artists))
}

// SearchItemProvider 在非默认 provider 的结果后追加来源标签。
func SearchItemProvider(n int, name, artists, providerName string) string {
	if providerName == "" {
		return SearchItem(n, name, artists)
	}
	return fmt.Sprintf("<code>%-3s</code> <code>%s - %s</code> <i>#%s</i>\n",
		fmt.Sprintf("%d.", n), Escape(name), Escape(artists), Escape(providerName))
}

// PluginLine 是 /status 中的一行插件信息，commit 与 buildTime 为空时省略。
func PluginLine(displayName, name, version, commit, buildTime, capabilities string) string {
	build := ""
	if len(commit) > 7 {
		commit = commit[:7]
	}
	if commit != "" || buildTime != "" {
		build = fmt.Sprintf(" [%s]", Escape(strings.TrimSpace(commit+" "+buildTime)))
	}
	return fmt.Sprintf("<b>[插件]</b> %s (%s) v%s%s 能力: %s", Escape(displayName), Escape(name), Escape(version), build, Escape(capabilities))
}

// AuthLine 是 /status 中的一行登录状态。
func AuthLine(name string, available bool) string {
	status := "正常"
	if !available {
		status = "已失效，新歌下载暂停"
	}
	return fmt.Sprintf("<b>[登录]</b> %s: %s", Escape(name), status)
}
