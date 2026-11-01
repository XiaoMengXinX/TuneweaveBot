package plugin

import (
	"testing"
)

func TestHostAllowed(t *testing.T) {
	allowed := []string{"music.163.com", "*.example.com"}
	for _, tt := range []struct {
		host string
		want bool
	}{
		{"music.163.com", true},
		{"MUSIC.163.COM.", true},
		{"api.example.com", true},
		{"example.com", false},
		{"evil.com", false},
	} {
		if got := hostAllowed(tt.host, allowed); got != tt.want {
			t.Errorf("hostAllowed(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
	if hostAllowed("anything", nil) {
		t.Error("空 allowlist 应拒绝所有 host")
	}
}

func TestPluginNameHint(t *testing.T) {
	for path, want := range map[string]string{
		"plugins/netease.wasm":     "netease",
		"plugins/My Plugin.WASM":   "myplugin",
		"plugins/---.wasm":         "probe",
		"plugins/qq_music.wasm":    "qq_music",
		"plugins/spotify-1.0.wasm": "spotify-10",
	} {
		if got := pluginNameHint(path); got != want {
			t.Errorf("pluginNameHint(%q) = %q, want %q", path, got, want)
		}
	}
}
