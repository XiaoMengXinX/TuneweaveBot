package render

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// HelpSource 是 /help 中列出的一个音乐来源（provider）。
type HelpSource struct {
	Name        string
	DisplayName string
	Code        string
	Default     bool
}

// HelpCommand 是插件声明的一个命令。
type HelpCommand struct {
	Name        string
	Usage       string
	Description string
}

// Help 列出所有面向普通用户的命令与可用来源。
func Help(sources []HelpSource, commands []HelpCommand, botUsername string) string {
	var b strings.Builder
	line := func(cmd, desc string) {
		fmt.Fprintf(&b, "%s — %s\n", Escape(cmd), desc)
	}

	b.WriteString("<b>🎵 点歌</b>\n")
	line("/music <ID/关键词/链接>", "用默认来源点歌")
	line("/music <来源> <ID/关键词>", "从指定来源点歌")
	for i, s := range sources {
		if i == 2 {
			break
		}
		fmt.Fprintf(&b, "　例：<code>/music %s 晴天</code>\n", Escape(s.Name))
	}
	b.WriteString("直接发送分享链接也能点歌\n")

	b.WriteString("\n<b>🔍 搜索</b>\n")
	line("/search [来源] <关键词>", "搜索歌曲，点数字按钮发送，可翻页")

	b.WriteString("\n<b>📝 歌词</b>\n")
	line("/lyric [来源] <ID/关键词>", "发送歌词文件；也可回复含分享链接的消息")

	b.WriteString("\n<b>🎤 听歌识曲</b>\n")
	line("/recognize", "回复一段含音乐的语音，识别是哪首歌")

	if len(commands) > 0 {
		b.WriteString("\n<b>🧩 插件命令</b>\n")
		for _, c := range commands {
			line(strings.TrimSpace("/"+c.Name+" "+c.Usage), Escape(c.Description))
		}
	}

	if len(sources) > 0 {
		b.WriteString("\n<b>📦 可用来源</b>（写插件名或短 code）\n")
		for _, s := range sources {
			fmt.Fprintf(&b, "%s：<code>%s</code> / <code>%s</code>", Escape(s.DisplayName), Escape(s.Name), Escape(s.Code))
			if s.Default {
				b.WriteString("（默认）")
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n<b>💬 其他</b>\n")
	line("/status", "缓存与插件状态")
	line("/about", "关于本 bot")
	line("/help", "显示本帮助")
	fmt.Fprintf(&b, "在任意聊天输入 <code>@%s 关键词或链接</code> 使用 inline 模式，输入 <code>@%s help</code> 查看用法",
		Escape(botUsername), Escape(botUsername))
	return b.String()
}

// PluginStatus 是 /plugin 中一个插件的状态。
type PluginStatus struct {
	Name         string
	DisplayName  string
	Version      string
	Build        string // commit 与构建时间，空表示未注入
	Capabilities []string
	Instances    int
	Default      bool
	Auth         string // 空表示插件不需要登录
	Updatable    bool
	Path         string
}

// PluginStatusList 汇总所有插件的状态。
func PluginStatusList(items []PluginStatus) string {
	var b strings.Builder
	for i, p := range items {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "<b>%s</b> (<code>%s</code>)", Escape(p.DisplayName), Escape(p.Name))
		if p.Default {
			b.WriteString(" · 默认")
		}
		build := p.Build
		if build == "" {
			build = "无构建信息"
		}
		fmt.Fprintf(&b, "\n版本：v%s（%s）\n", Escape(p.Version), Escape(build))
		fmt.Fprintf(&b, "能力：%s · 常驻实例 %d\n", Escape(strings.Join(p.Capabilities, "/")), p.Instances)
		if p.Auth != "" {
			fmt.Fprintf(&b, "登录：%s\n", Escape(p.Auth))
		}
		update := "未声明更新地址"
		if p.Updatable {
			update = "已启用"
		}
		fmt.Fprintf(&b, "自动更新：%s\n", update)
		fmt.Fprintf(&b, "文件：<code>%s</code>\n", Escape(p.Path))
	}
	b.WriteString("\n发送 <code>/plugin 插件名</code> 查看 manifest")
	return b.String()
}

// manifestLimit 给 Telegram 4096 字符的消息上限留出标签与提示的余量。
const manifestLimit = 3800

// PluginManifest 以 JSON 代码块展示 manifest，超长时截断。
func PluginManifest(displayName, name string, manifest []byte) string {
	text := string(manifest)
	truncated := false
	if utf8.RuneCountInString(text) > manifestLimit {
		text = string([]rune(text)[:manifestLimit])
		truncated = true
	}
	out := fmt.Sprintf("<b>%s</b> (<code>%s</code>) manifest\n<pre><code class=\"language-json\">%s</code></pre>",
		Escape(displayName), Escape(name), Escape(text))
	if truncated {
		out += "\n（内容过长，已截断）"
	}
	return out
}

func PluginNotFound(name string) string {
	return fmt.Sprintf("未找到插件 <code>%s</code>，发送 /plugin 查看已安装的插件", Escape(name))
}
