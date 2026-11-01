package plugin

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm/logger"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

func TestSmokeNeteaseWasm(t *testing.T) {
	src := os.Getenv("NETEASE_WASM")
	if src == "" {
		t.Skip("NETEASE_WASM not set")
	}
	dir := t.TempDir()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(dir, "netease.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	_ = in.Close()
	_ = out.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "cache.db"), logger.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)
	m, err := Load(context.Background(), Config{
		Dir:     dir,
		Default: "netease",
		Configs: map[string]json.RawMessage{"netease": json.RawMessage(`{"level":"hires"}`)},
	}, st, log, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())

	for _, text := range []string{"123", "https://music.163.com/song?id=456", "https://music.163.com/program?id=2061"} {
		ref, _, ok := m.Providers().Match(context.Background(), text)
		t.Logf("match %q -> %+v %v", text, ref, ok)
		if !ok {
			t.Errorf("match %q failed", text)
		}
	}
	t.Logf("info: %+v", m.Info())
	t.Logf("commands: %+v", m.Commands())

	track, err := m.Providers().Resolve(context.Background(), provider.Ref{Provider: "netease", ID: "347230"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Logf("track: %s / %v / %s / %s", track.Title, track.Artists, track.AudioURL, track.PageURL)

	items, total, err := m.Providers().Search(context.Background(), "", "周杰伦", 3, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	t.Logf("search total=%d items=%d first=%+v", total, len(items), items[0])
}
