package config

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validTOML = `
[bot]
token = "123456:ABC"
admin = [115414, 1919810]

[plugins.netease]
music_u = "COOKIE"

[download]
timeout = "90s"
retries = 5
`

func TestLoadDefaults(t *testing.T) {
	path := writeTemp(t, `
[bot]
token = "123456:ABC"

[plugins.netease]
music_u = "COOKIE"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.APIURL != "https://api.telegram.org" {
		t.Errorf("api_url 默认值错误: %s", cfg.Bot.APIURL)
	}
	if cfg.Database.Path != "cache.db" {
		t.Errorf("database.path 默认值错误: %s", cfg.Database.Path)
	}
	if cfg.Download.Timeout.D() != 60*time.Second {
		t.Errorf("timeout 默认值错误: %v", cfg.Download.Timeout)
	}
	if cfg.Download.Retries != 3 {
		t.Errorf("retries 默认值错误: %d", cfg.Download.Retries)
	}
	if cfg.Download.Concurrency != 4 {
		t.Errorf("concurrency 默认值错误: %d", cfg.Download.Concurrency)
	}
	if cfg.Log.Level != "info" || cfg.Log.Path != "./log" {
		t.Errorf("log 默认值错误: %+v", cfg.Log)
	}
}

func TestUploadTimeout(t *testing.T) {
	t.Setenv("TUNEWEAVE_BOT_UPLOAD_TIMEOUT", "")
	if err := os.Unsetenv("TUNEWEAVE_BOT_UPLOAD_TIMEOUT"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		setting string
		env     string
		want    time.Duration
		wantErr bool
	}{
		{name: "default", want: 120 * time.Second},
		{name: "toml", setting: `upload_timeout = "5m"`, want: 5 * time.Minute},
		{name: "env", setting: `upload_timeout = "5m"`, env: "180s", want: 180 * time.Second},
		{name: "zero", setting: `upload_timeout = "0s"`, wantErr: true},
		{name: "negative", setting: `upload_timeout = "-1s"`, wantErr: true},
		{name: "invalid", setting: `upload_timeout = "invalid"`, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("TUNEWEAVE_BOT_UPLOAD_TIMEOUT", tt.env)
			}
			cfg, err := Load(writeTemp(t, "[bot]\ntoken = '1:test'\n"+tt.setting))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected invalid upload timeout error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Bot.UploadTimeout.D() != tt.want {
				t.Errorf("upload_timeout = %v, want %v", cfg.Bot.UploadTimeout.D(), tt.want)
			}
		})
	}
}

func TestLocalMode(t *testing.T) {
	t.Setenv("TUNEWEAVE_BOT_LOCAL_MODE", "")
	if err := os.Unsetenv("TUNEWEAVE_BOT_LOCAL_MODE"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		setting string
		env     string
		want    bool
		wantErr bool
	}{
		{name: "default"},
		{name: "toml", setting: "local_mode = true", want: true},
		{name: "env enabled", env: "true", want: true},
		{name: "env disabled", setting: "local_mode = true", env: "false"},
		{name: "invalid env", env: "invalid", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("TUNEWEAVE_BOT_LOCAL_MODE", tt.env)
			}
			cfg, err := Load(writeTemp(t, "[bot]\ntoken = '1:test'\n"+tt.setting))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected invalid boolean error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Bot.LocalMode != tt.want {
				t.Errorf("local_mode = %v, want %v", cfg.Bot.LocalMode, tt.want)
			}
		})
	}
}

func TestLoadValues(t *testing.T) {
	cfg, err := Load(writeTemp(t, validTOML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Token != "123456:ABC" {
		t.Errorf("token = %s", cfg.Bot.Token)
	}
	if len(cfg.Bot.Admin) != 2 || cfg.Bot.Admin[0] != 115414 || cfg.Bot.Admin[1] != 1919810 {
		t.Errorf("admin = %v", cfg.Bot.Admin)
	}
	if cfg.Download.Timeout.D() != 90*time.Second {
		t.Errorf("timeout = %v", cfg.Download.Timeout)
	}
	if cfg.Download.Retries != 5 {
		t.Errorf("retries = %d", cfg.Download.Retries)
	}
}

func TestLoadUnknownFieldRejected(t *testing.T) {
	_, err := Load(writeTemp(t, validTOML+"\nbadkey = 1\n"))
	if err == nil || !strings.Contains(err.Error(), "strict mode") {
		t.Fatalf("拼错的配置项应被严格模式拒绝: %v", err)
	}
	// 即使存在被剥离的插件段，其它段的拼写错误仍应被拒绝
	_, err = Load(writeTemp(t, `[bot]
token = "x"
badkey = 1

[plugins.netease]
level = "hires"
`))
	if err == nil || !strings.Contains(err.Error(), "strict mode") {
		t.Fatalf("剥离插件段后拼错项仍应被严格模式拒绝: %v", err)
	}
}

func TestNeteaseLevel(t *testing.T) {
	cfg, err := Load(writeTemp(t, `[bot]
token = "123:ABC"

[plugins.netease]
music_u = "COOKIE"
level = "lossless"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins.Config["netease"]["level"] != "lossless" {
		t.Errorf("level = %v, want lossless", cfg.Plugins.Config["netease"]["level"])
	}
	cfg, err = Load(writeTemp(t, `[bot]
token = "123:ABC"

[plugins.netease]
music_u = "COOKIE"
level = "sq"
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins.Config["netease"]["level"] != "sq" {
		t.Errorf("level = %v, want sq", cfg.Plugins.Config["netease"]["level"])
	}
}

func TestNeteaseProxy(t *testing.T) {
	t.Setenv("TUNEWEAVE_PLUGINS_PROXY", "")
	if err := os.Unsetenv("TUNEWEAVE_PLUGINS_PROXY"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name    string
		setting string
		env     *string
		want    string
		wantErr bool
	}{
		{name: "default"},
		{name: "toml", setting: `proxy = "http://localhost:8080"`, want: "http://localhost:8080"},
		{name: "env", setting: `proxy = "invalid"`, env: new("socks5://user:password@localhost:1080"), want: "socks5://user:password@localhost:1080"},
		{name: "empty env", setting: `proxy = "http://localhost:8080"`, env: new("")},
		{name: "invalid env", env: new("http://user:password@localhost:0"), wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != nil {
				t.Setenv("TUNEWEAVE_PLUGINS_PROXY", *tt.env)
			}
			cfg, err := Load(writeTemp(t, "[bot]\ntoken = '1:test'\n[plugins]\n"+tt.setting))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected invalid proxy error")
				}
				if strings.Contains(err.Error(), "password") {
					t.Fatalf("proxy error leaked credentials: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Plugins.Proxy != tt.want {
				t.Error("proxy configuration mismatch")
			}
		})
	}
}

func TestNeteaseProxyValidation(t *testing.T) {
	for _, proxy := range []string{
		"", "http://localhost", "https://localhost:443/", "socks5://localhost:1080",
		"socks5h://localhost", "http://user:password@proxy.example:1",
		"socks5://user:p%40ssword@127.0.0.1:65535", "http://[::1]", "http://[::1]:8080/",
	} {
		t.Run("valid/"+proxy, func(t *testing.T) {
			cfg := Default()
			cfg.Bot.Token = "1:test"
			cfg.Plugins.Proxy = proxy
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, proxy := range []string{
		"localhost:8080", "//localhost:8080", "ftp://localhost", "http:localhost",
		"http://", "http://:8080", "http://user:password@:8080", "http:///localhost",
		"http://localhost:0", "http://localhost:65536", "http://localhost:-1",
		"http://localhost:abc", "http://localhost:", "http://[::1]:", "http://::1",
		"http://localhost/path", "http://localhost//", "http://localhost/%2F", "http://localhost/%2f",
		"http://localhost?query", "http://localhost?", "http://localhost#fragment", "http://localhost#",
		"http://user:password@localhost:bad", "http://user:password@localhost/%zz", "http://user:password@bad host",
	} {
		t.Run("invalid/"+proxy, func(t *testing.T) {
			cfg := Default()
			cfg.Bot.Token = "1:test"
			cfg.Plugins.Proxy = proxy
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected invalid proxy error")
			}
			if !strings.Contains(err.Error(), "[plugins] proxy") || strings.Contains(err.Error(), proxy) || strings.Contains(err.Error(), "password") {
				t.Fatalf("unexpected proxy error: %v", err)
			}
		})
	}
}

// neteasePlugins 返回只配置了 proxy 的 PluginsConfig，供 HTTP 客户端测试使用。
func neteasePlugins(proxy string) PluginsConfig {
	return PluginsConfig{Proxy: proxy}
}

func TestNeteaseHTTPClient(t *testing.T) {
	client, err := neteasePlugins("").HTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	if client.Transport != nil || client.Timeout != 0 {
		t.Fatal("empty proxy should preserve default transport and timeout")
	}
	if client, err := neteasePlugins("http://user:password@localhost:bad").HTTPClient(); err == nil || client != nil || strings.Contains(err.Error(), "password") {
		t.Fatal("parse errors should be returned without credentials")
	}

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "http://target.invalid/song?id=1" {
			t.Errorf("unexpected proxy target: %s", r.URL)
		}
		if r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNzd29yZA==" {
			t.Error("missing proxy authentication")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	defaultTransport := http.DefaultTransport.(*http.Transport)
	defaultProxy := fmt.Sprintf("%p", defaultTransport.Proxy)
	cfg := Default()
	cfg.Bot.Token = "1:test"
	cfg.Plugins.Proxy = strings.Replace(proxy.URL, "://", "://user:password@", 1)
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	client, err = cfg.Plugins.HTTPClient()
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == defaultTransport || client.Timeout != 0 {
		t.Fatal("explicit proxy should clone transport without adding a timeout")
	}
	if fmt.Sprintf("%p", defaultTransport.Proxy) != defaultProxy {
		t.Fatal("default transport proxy was modified")
	}
	resp, err := client.Get("http://target.invalid/song?id=1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("proxy response status = %d", resp.StatusCode)
	}
}

func TestNeteaseHTTPClientSOCKS5(t *testing.T) {
	for _, scheme := range []string{"socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			handshake := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					handshake <- err
					return
				}
				defer conn.Close()
				if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
					handshake <- err
					return
				}
				var greeting [2]byte
				if _, err := io.ReadFull(conn, greeting[:]); err != nil {
					handshake <- err
					return
				}
				methods := make([]byte, int(greeting[1]))
				if _, err := io.ReadFull(conn, methods); err != nil {
					handshake <- err
					return
				}
				if greeting[0] != 5 || len(methods) == 0 || methods[0] != 0 {
					handshake <- fmt.Errorf("unexpected SOCKS5 greeting: %v %v", greeting, methods)
					return
				}
				_, err = conn.Write([]byte{5, 255})
				handshake <- err
			}()
			cfg := Default()
			cfg.Bot.Token = "1:test"
			cfg.Plugins.Proxy = scheme + "://" + listener.Addr().String()
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
			client, err := cfg.Plugins.HTTPClient()
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			client.Timeout = 5 * time.Second
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("failed SOCKS5 proxy must not fall back to a direct request")
			}))
			defer target.Close()
			if resp, err := client.Get(target.URL); err == nil {
				resp.Body.Close()
				t.Error("SOCKS5 rejection should fail the request")
			}
			listener.Close()
			select {
			case err := <-handshake:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("SOCKS5 handshake was not received")
			}
		})
	}
}

func TestLoadMissingRequired(t *testing.T) {
	if _, err := Load(writeTemp(t, `[plugins.netease]
music_u = "x"
`)); err == nil {
		t.Error("缺少 token 应报错")
	}
	if _, err := Load(writeTemp(t, `[bot]
token = "x"
`)); err != nil {
		t.Errorf("music_u 留空不应报错: %v", err)
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("TUNEWEAVE_BOT_TOKEN", "env-token")
	t.Setenv("TUNEWEAVE_BOT_ADMIN", "1, 2")
	t.Setenv("TUNEWEAVE_DOWNLOAD_TIMEOUT", "120s")
	cfg, err := Load(writeTemp(t, validTOML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.Token != "env-token" {
		t.Errorf("token 环境变量覆盖失败: %s", cfg.Bot.Token)
	}
	if len(cfg.Bot.Admin) != 2 || cfg.Bot.Admin[1] != 2 {
		t.Errorf("admin 环境变量覆盖失败: %v", cfg.Bot.Admin)
	}
	if cfg.Download.Timeout.D() != 120*time.Second {
		t.Errorf("timeout 环境变量覆盖失败: %v", cfg.Download.Timeout)
	}
}

func TestPluginsConfig(t *testing.T) {
	cfg, err := Load(writeTemp(t, `
[bot]
token = "123456:ABC"
admin = [115414, 1919810]

[plugins]
dir = "./myplugins"
enabled = ["netease", "qq"]
default = "netease"
instances = 3
call_timeout = "10s"
memory_mb = 256
http_max_bytes = 1048576
allow_private_networks = true

[plugins.netease]
music_u = "COOKIE"
level = "lossless"
`))
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Plugins
	if p.Dir != "./myplugins" || p.Default != "netease" || p.Instances != 3 || p.MemoryMB != 256 {
		t.Errorf("plugins = %+v", p)
	}
	if p.CallTimeout.D() != 10*time.Second || p.HTTPMaxBytes != 1048576 || !p.AllowPrivateNetworks {
		t.Errorf("plugins runtime = %+v", p)
	}
	if len(p.Enabled) != 2 || p.Enabled[0] != "netease" || p.Enabled[1] != "qq" {
		t.Errorf("enabled = %v", p.Enabled)
	}
	if got := p.Config["netease"]["level"]; got != "lossless" {
		t.Errorf("netease level = %v", got)
	}
	if got := p.Config["netease"]["music_u"]; got != "COOKIE" {
		t.Errorf("netease music_u = %v, want COOKIE", got)
	}
}

func TestPluginsSectionReservedForHost(t *testing.T) {
	// [plugins] 宿主字段拼写错误仍应被严格模式拒绝
	if _, err := Load(writeTemp(t, validTOML+"\n[plugins]\nbadkey = 1\n")); err == nil {
		t.Error("[plugins] 中拼错的宿主字段应被拒绝")
	}
}

func TestPluginsCustomSection(t *testing.T) {
	// 任意插件段原样透传，不受 netease 专属逻辑影响
	cfg, err := Load(writeTemp(t, `
[bot]
token = "123456:ABC"

[plugins.netease]
music_u = "COOKIE"
level = "hires"

[plugins.example]
greeting = "hi"
count = 7
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Plugins.Config["example"]["greeting"]; got != "hi" {
		t.Errorf("example greeting = %v", got)
	}
	if got, ok := cfg.Plugins.Config["example"]["count"].(int64); !ok || got != 7 {
		t.Errorf("example count = %v (%T)", cfg.Plugins.Config["example"]["count"], cfg.Plugins.Config["example"]["count"])
	}
}

func TestPluginsEnvOverride(t *testing.T) {
	t.Setenv("TUNEWEAVE_PLUGINS_DIR", "./envplugins")
	t.Setenv("TUNEWEAVE_PLUGINS_ENABLED", "netease, qq")
	t.Setenv("TUNEWEAVE_PLUGINS_CONFIG_NETEASE_LEVEL", "sky")
	cfg, err := Load(writeTemp(t, validTOML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins.Dir != "./envplugins" {
		t.Errorf("dir = %q", cfg.Plugins.Dir)
	}
	if len(cfg.Plugins.Enabled) != 2 || cfg.Plugins.Enabled[1] != "qq" {
		t.Errorf("enabled = %v", cfg.Plugins.Enabled)
	}
	if got := cfg.Plugins.Config["netease"]["level"]; got != "sky" {
		t.Errorf("netease level = %v", got)
	}
}

func TestPluginsValidation(t *testing.T) {
	for name, extra := range map[string]string{
		"dir":            "[plugins]\ndir = \"\"\n",
		"instances":      "[plugins]\ninstances = 0\n",
		"call_timeout":   "[plugins]\ncall_timeout = \"0s\"\n",
		"memory_mb":      "[plugins]\nmemory_mb = 0\n",
		"http_max_bytes": "[plugins]\nhttp_max_bytes = 0\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, validTOML+"\n"+extra)); err == nil {
				t.Errorf("%s 非法值应报错", name)
			}
		})
	}
}
