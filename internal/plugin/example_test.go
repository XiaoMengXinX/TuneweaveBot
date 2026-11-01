package plugin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm/logger"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

// TestExamplePlugin 现场构建 plugins/example 并加载，验证“加插件不改宿主”。
func TestExamplePlugin(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	wasm := filepath.Join(dir, "example.wasm")
	cmd := exec.Command("go", "build", "-buildmode=c-shared", "-trimpath", "-ldflags=-s -w", "-o", wasm, "./plugins/example")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=wasip1", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build example plugin: %v\n%s", err, out)
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "cache.db"), logger.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	m, err := Load(context.Background(), Config{
		Dir:     dir,
		Default: "example",
		Configs: map[string]json.RawMessage{"example": json.RawMessage(`{"audio_url_template":"https://example.com/audio/{id}.mp3"}`)},
	}, st, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())

	info := m.Info()
	if len(info) != 1 || info[0].Name != "example" || info[0].DisplayName != "示例音乐" {
		t.Fatalf("info = %+v", info)
	}
	commands := m.Commands()
	if len(commands) != 1 || commands[0].Plugin != "example" || commands[0].Command.Name != "example" {
		t.Fatalf("commands = %+v", commands)
	}

	ref, p, ok := m.Providers().Match(context.Background(), "ex:42")
	if !ok || ref.ID != "42" || p.Name() != "example" {
		t.Fatalf("match = %+v, %v", ref, ok)
	}
	track, err := m.Providers().Resolve(context.Background(), provider.Ref{Provider: "example", ID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	if track.Title != "示例歌曲 42" || track.AudioURL != "https://example.com/audio/42.mp3" || track.ProviderName != "示例音乐" {
		t.Errorf("track = %+v", track)
	}
	items, total, err := m.Providers().Search(context.Background(), "example", "demo", 3, 0)
	if err != nil || total != 42 || len(items) != 3 || items[0].ID != "1" {
		t.Fatalf("search = %+v, %d, %v", items, total, err)
	}
	lyric, err := m.Providers().Lyrics(context.Background(), provider.Ref{Provider: "example", ID: "42"})
	if err != nil || lyric.Title != "示例歌曲 42" {
		t.Fatalf("lyrics = %+v, %v", lyric, err)
	}
}
