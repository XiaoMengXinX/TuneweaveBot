package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdatePluginValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := `# 我的配置注释
[bot]
token = "111:AAA" # token 注释
admin = [1, 2]

[plugins.netease]
# music_u 注释
music_u = 'old-cookie' # 行尾注释
level = "hires"

[download]
timeout = "60s"
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpdatePluginValue(path, "netease", "music_u", "new-cookie<>&"); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `# 我的配置注释
[bot]
token = "111:AAA" # token 注释
admin = [1, 2]

[plugins.netease]
# music_u 注释
music_u = "new-cookie<>&" # 行尾注释
level = "hires"

[download]
timeout = "60s"
`
	if string(after) != want {
		t.Errorf("回写结果不符合预期:\n%s", after)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Plugins.Config["netease"]["music_u"]; got != "new-cookie<>&" {
		t.Errorf("music_u = %q", got)
	}
	if cfg.Bot.Token != "111:AAA" || cfg.Download.Timeout.D() != 60*time.Second {
		t.Errorf("其它配置被误改: %+v", cfg)
	}

	if err := UpdatePluginValue(path, "netease", "music_u", "third"); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Plugins.Config["netease"]["music_u"]; got != "third" {
		t.Errorf("二次更新失败: %q", got)
	}
}

func TestUpdatePluginValueInsert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[bot]\ntoken = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 段与键都不存在时应新建而非报错
	if err := UpdatePluginValue(path, "demo", "token", "COOKIE"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Plugins.Config["demo"]["token"]; got != "COOKIE" {
		t.Errorf("token = %q", got)
	}
}

func TestUpdatePluginValueExistingSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	src := "[bot]\ntoken = \"x\"\n\n[plugins.demo]\nquality = \"high\"\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// 段存在但键缺失时在段内补齐
	if err := UpdatePluginValue(path, "demo", "token", "COOKIE"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Plugins.Config["demo"]["token"]; got != "COOKIE" {
		t.Errorf("token = %q", got)
	}
	if got := cfg.Plugins.Config["demo"]["quality"]; got != "high" {
		t.Errorf("原有键被破坏: quality = %q", got)
	}
}

func TestUpdateCacheChat(t *testing.T) {
	for _, tt := range []struct {
		name     string
		original string
		chatID   int64
		want     string
	}{
		{
			name:     "insert",
			original: "[bot] # bot 注释\ntoken = \"x\"\n\n[netease]\nmusic_u = \"\"\n",
			chatID:   -100123,
			want:     "[bot] # bot 注释\ncache_chat = -100123\ntoken = \"x\"\n\n[netease]\nmusic_u = \"\"\n",
		},
		{
			name:     "replace",
			original: "[bot]\ntoken = \"x\"\ncache_chat = -100123 # 缓存群\n",
			chatID:   0,
			want:     "[bot]\ntoken = \"x\"\ncache_chat = 0 # 缓存群\n",
		},
		{
			name:     "other section",
			original: "[log]\ncache_chat = 1\n[bot]\ntoken = \"x\"\n",
			chatID:   42,
			want:     "[log]\ncache_chat = 1\n[bot]\ncache_chat = 42\ntoken = \"x\"\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tt.original), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := UpdateCacheChat(path, tt.chatID); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != tt.want {
				t.Errorf("回写结果不符合预期:\n%s", after)
			}
		})
	}
}

func TestUpdateCacheChatLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[bot]\ntoken = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpdateCacheChat(path, -100123); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.CacheChat != -100123 {
		t.Errorf("cache_chat = %d", cfg.Bot.CacheChat)
	}
}

func TestUpdateCacheChatMissingSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[netease]\nmusic_u = \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpdateCacheChat(path, 1); err == nil {
		t.Error("缺少 [bot] 应报错")
	}
}
