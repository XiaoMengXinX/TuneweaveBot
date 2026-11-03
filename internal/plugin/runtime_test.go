package plugin

import (
	"context"
	"strings"

	"github.com/sirupsen/logrus"
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
		"plugins/netease.wasm":   "netease",
		"plugins/My Plugin.WASM": "myplugin",
		"plugins/---.wasm":       "probe",
		"plugins/qq_music.wasm":  "qq_music",
		"plugins/demo-1.0.wasm":  "demo-10",
	} {
		if got := pluginNameHint(path); got != want {
			t.Errorf("pluginNameHint(%q) = %q, want %q", path, got, want)
		}
	}
}

// 连接失败时只回传底层原因，方法与地址由插件的 http.Client 添加。
func TestHTTPRequestErrorOmitsURL(t *testing.T) {
	s := &instanceState{
		plugin:       "demo",
		allowedHosts: []string{"127.0.0.1"},
		maxHTTPBytes: 1 << 20,
		httpClient:   pluginHTTPClient(nil, true),
		log:          logrus.New(),
	}
	_, err := s.httpRequest(context.Background(), hostHTTPRequest{Method: "GET", URL: "http://127.0.0.1:1/path"})
	if err == nil {
		t.Fatal("连接被拒绝时应报错")
	}
	if strings.Contains(err.Error(), "127.0.0.1:1/path") || strings.Contains(err.Error(), "GET") {
		t.Errorf("err = %q，不应包含方法与地址", err)
	}
}
