package textx

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestURLBase(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://m801.music.126.net/abc/def.mp3?auth=1&x=2", "def.mp3"},
		{"https://p1.music.126.net/xxx/cover.jpg", "cover.jpg"},
		{"https://p1.music.126.net/xxx/cover.jpg#frag", "cover.jpg"},
	}
	for _, tt := range tests {
		if got := URLBase(tt.url); got != tt.want {
			t.Errorf("URLBase(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	got := SanitizeFilename("a/b\\c*d:e?f\"g<h>i|j\nk\r")
	want := "a b c d e f g h i j k "
	if got != want {
		t.Errorf("SanitizeFilename = %q, want %q", got, want)
	}
}

func TestSanitizeFilenameLength(t *testing.T) {
	artists := strings.Repeat("影山ヒロノブ,ささきいさお,", 8)
	for _, tt := range []struct {
		name string
		ext  string
	}{
		{name: artists + " - 秘密戦隊ゴレンジャー [スーパー戦隊“魂”2004 LIVE].mp3", ext: ".mp3"},
		{name: "a" + artists + " - 愿世界和平.flac", ext: ".flac"},
		{name: "ab" + artists + " - 愿世界和平.lrc", ext: ".lrc"},
	} {
		got := SanitizeFilename(tt.name)
		if len(got) > maxFilenameBytes || !utf8.ValidString(got) || !strings.HasSuffix(got, tt.ext) {
			t.Errorf("SanitizeFilename(%q) = %q (%d bytes)", tt.name, got, len(got))
		}
		if base := strings.TrimSuffix(got, tt.ext); !strings.HasPrefix(tt.name, base) || len(got) < maxFilenameBytes-3 {
			t.Errorf("SanitizeFilename(%q) = %q, want longest prefix", tt.name, got)
		}
	}
	exact := strings.Repeat("a", maxFilenameBytes-len(".flac")) + ".flac"
	if got := SanitizeFilename(exact); got != exact {
		t.Errorf("%d-byte name changed: %q", maxFilenameBytes, got)
	}
}

func TestLrcText(t *testing.T) {
	lrc := "[00:00.000] 作词 : 周杰伦\n[00:01.053]\n\n[00:28.950]故事的小黄花\n[01:00.00][02:00.00]重复的副歌  \n纯文本"
	if got, want := LrcText(lrc), "作词 : 周杰伦\n故事的小黄花\n重复的副歌\n纯文本"; got != want {
		t.Errorf("LrcText = %q, want %q", got, want)
	}
	if got := LrcText("[00:00.00]\n[00:01.00] "); got != "" {
		t.Errorf("tag-only lyric = %q, want empty", got)
	}
}
