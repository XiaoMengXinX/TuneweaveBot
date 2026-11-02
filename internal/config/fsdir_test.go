package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFSDirParses(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.toml")
	toml := `
[bot]
token = "1:AAA"
admin = [1]
[plugins]
dir = "./plugins"
instances = 1
call_timeout = "30s"
memory_mb = 256
http_max_bytes = 16777216
fs_dir = "./plugin-fs"
[plugins.demo]
token = "x"
[database]
path = "cache.db"
`
	if err := os.WriteFile(p, []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Plugins.FSDir != "./plugin-fs" {
		t.Fatalf("FSDir=%q", c.Plugins.FSDir)
	}
	if _, ok := c.Plugins.Config["demo"]; !ok {
		t.Fatalf("demo section lost: %+v", c.Plugins.Config)
	}
	if _, ok := c.Plugins.Config["fs_dir"]; ok {
		t.Fatalf("fs_dir treated as plugin section")
	}
}
