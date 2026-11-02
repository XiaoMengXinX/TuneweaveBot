package render

import (
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func TestEscapeInTemplates(t *testing.T) {
	caption := MusicCaption("<b>歌名&</b>", "歌手", "专辑", "mp3", 1.5, 320, "网易云音乐", "TuneweaveBot")
	if strings.Contains(caption, "<b>歌名") {
		t.Errorf("caption 未转义: %s", caption)
	}
	if !strings.Contains(caption, "&lt;b&gt;歌名&amp;&lt;/b&gt;") {
		t.Errorf("caption 转义结果错误: %s", caption)
	}

	info := SongInfo("雨き声残響", "ゆめこ & <歌手>", "最新热歌慢摇92")
	if info != "「雨き声残響」- ゆめこ &amp; &lt;歌手&gt;\n专辑: 最新热歌慢摇92" {
		t.Errorf("歌曲信息格式错误: %s", info)
	}
	if got := RecognizeResult("雨き声残響", "ゆめこ & <歌手>", "最新热歌慢摇92"); got != "🎵 识别成功\n「雨き声残響」- ゆめこ &amp; &lt;歌手&gt;\n专辑: 最新热歌慢摇92" {
		t.Errorf("识曲结果格式错误: %s", got)
	}
	if caption != "「&lt;b&gt;歌名&amp;&lt;/b&gt;」- 歌手\n专辑: 专辑\n#网易云音乐 #mp3 1.50MB 320.00kbps\nvia @TuneweaveBot" {
		t.Errorf("caption 格式错误: %s", caption)
	}

	status := MusicStatus("a<b", "c&d", "mp3", 1.0, Downloading)
	if !strings.Contains(status, "a&lt;b") || !strings.Contains(status, "c&amp;d") {
		t.Errorf("状态消息未转义: %s", status)
	}

	about := About("go1.22", "v3<a", "sha", "2024", "darwin/arm64", []string{"netease"})
	if strings.Contains(about, "v3<a") {
		t.Errorf("about 未转义: %s", about)
	}

	info = StatusInfo("群<名>", "group_link", 1, 2, 3, 4, 5)
	if strings.Contains(info, "群<名>") {
		t.Errorf("status 未转义: %s", info)
	}
	if !strings.Contains(info, `href="https://t.me/group_link"`) {
		t.Errorf("群聊应生成 t.me 链接: %s", info)
	}
}

func TestStatusInfoChatVariants(t *testing.T) {
	private := StatusInfo("", "alice", 42, 42, 1, 1, 1)
	if !strings.Contains(private, `tg://user?id=42`) {
		t.Errorf("私聊应生成 tg://user 链接: %s", private)
	}
	plain := StatusInfo("群名", "", 1, 2, 3, 4, 5)
	if !strings.Contains(plain, "[群名]") {
		t.Errorf("匿名群应展示标题: %s", plain)
	}
}

func TestDownloadStatusFormat(t *testing.T) {
	got := DownloadStatus("1.5MB/s", 3.0, 10.0, 30)
	want := " 1.5MB/s\n3.00MB/10.00MB 30%"
	if got != want {
		t.Errorf("DownloadStatus = %q, want %q", got, want)
	}
}

func TestLyricCaption(t *testing.T) {
	if got := LyricCaption("<a> & b"); got != "<blockquote expandable>&lt;a&gt; &amp; b</blockquote>" {
		t.Errorf("LyricCaption = %q", got)
	}
	for _, tt := range []struct {
		name      string
		lyric     string
		truncated bool
	}{
		{name: "exact limit", lyric: strings.Repeat("a", 1024)},
		{name: "ascii", lyric: strings.Repeat("a", 1500), truncated: true},
		{name: "cjk", lyric: strings.Repeat("歌词\n", 400), truncated: true},
		{name: "surrogate pairs", lyric: "a" + strings.Repeat("🎵", 600), truncated: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			text := strings.TrimSuffix(strings.TrimPrefix(LyricCaption(tt.lyric), "<blockquote expandable>"), "</blockquote>")
			length := len(utf16.Encode([]rune(text)))
			if !utf8.ValidString(text) || length > 1024 {
				t.Fatalf("caption text has %d UTF-16 units: %q", length, text)
			}
			if !tt.truncated {
				if text != tt.lyric {
					t.Errorf("caption changed: %q", text)
				}
				return
			}
			if !strings.HasSuffix(text, "…") || !strings.HasPrefix(tt.lyric, strings.TrimSuffix(text, "…")) || length < 1023 {
				t.Errorf("caption = %q (%d units), want longest prefix with ellipsis", text, length)
			}
		})
	}
}

func TestSearchItem(t *testing.T) {
	for _, tt := range []struct {
		n    int
		want string
	}{
		{n: 1, want: "<code>1. </code> <code>&lt;Song&gt; - A&amp;B</code>\n"},
		{n: 10, want: "<code>10.</code> <code>&lt;Song&gt; - A&amp;B</code>\n"},
	} {
		if got := SearchItem(tt.n, "<Song>", "A&B"); got != tt.want {
			t.Errorf("SearchItem(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
	if got := SearchPage(2, 5); got != "<code>第[2/5]页</code>\n" {
		t.Errorf("SearchPage = %q", got)
	}
}

func TestPluginManifest(t *testing.T) {
	out := PluginManifest("网易云<音乐>", "netease", []byte(`{"name":"<x>"}`))
	if !strings.Contains(out, "&lt;x&gt;") || strings.Contains(out, "<x>") || !strings.Contains(out, "网易云&lt;音乐&gt;") {
		t.Errorf("manifest 未转义: %s", out)
	}
	long := PluginManifest("n", "n", []byte(strings.Repeat("字", manifestLimit+10)))
	if !strings.Contains(long, "已截断") || utf8.RuneCountInString(long) > 4096 {
		t.Errorf("超长 manifest 未截断，长度 %d", utf8.RuneCountInString(long))
	}
}

func TestPluginStatusList(t *testing.T) {
	out := PluginStatusList([]PluginStatus{
		{Name: "netease", DisplayName: "网易云音乐", Version: "1.0.0", Build: "7f302d5 2026-11-02T21:50:19Z",
			Capabilities: []string{"provider", "auth"}, Instances: 2, Default: true, Auth: "正常", Updatable: true, Path: "plugins/netease.wasm.gz"},
		{Name: "example", DisplayName: "示例", Version: "0.1.0", Capabilities: []string{"provider"}, Instances: 1, Path: "plugins/example.wasm"},
	})
	for _, want := range []string{"网易云音乐</b> (<code>netease</code>) · 默认", "v1.0.0（7f302d5 2026-11-02T21:50:19Z）",
		"常驻实例 2", "登录：正常", "自动更新：已启用", "v0.1.0（无构建信息）", "自动更新：未声明更新地址", "/plugin 插件名"} {
		if !strings.Contains(out, want) {
			t.Errorf("状态缺少 %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "登录：") != 1 {
		t.Errorf("不需要登录的插件不应显示登录状态:\n%s", out)
	}
}

func TestStatusLines(t *testing.T) {
	if got := PluginLine("网易云音乐", "netease", "1.0.0", "7f302d5 2026-11-02T22:00:55Z"); got != "网易云音乐 (netease) v1.0.0 [7f302d5 2026-11-02T22:00:55Z]" {
		t.Errorf("PluginLine = %q", got)
	}
	if got := PluginLine("示例", "example", "0.1.0", ""); got != "示例 (example) v0.1.0" {
		t.Errorf("无构建信息时不应显示括号: %q", got)
	}
	if got := AuthLine("example", true); got != "example: 正常" {
		t.Errorf("AuthLine = %q", got)
	}
}
