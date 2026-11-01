// Package config 加载并校验 TOML 配置
package config

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/sirupsen/logrus"
)

// Duration 时长类型，接受 "60s" 这类格式
type Duration time.Duration

func (d Duration) D() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", string(b), err)
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalText() ([]byte, error) { return []byte(time.Duration(d).String()), nil }

type BotConfig struct {
	Token         string   `toml:"token"`
	APIURL        string   `toml:"api_url"`
	Admin         []int64  `toml:"admin"`
	Debug         bool     `toml:"debug"`
	LocalMode     bool     `toml:"local_mode"`
	UploadTimeout Duration `toml:"upload_timeout"`
	CacheChat     int64    `toml:"cache_chat"`
}

type NeteaseConfig struct {
	MUSICU string `toml:"music_u"`
	Level  string `toml:"level"`
	Proxy  string `toml:"proxy"`
}

func (c NeteaseConfig) proxyURL() (*url.URL, error) {
	if c.Proxy == "" {
		return nil, nil
	}
	invalid := fmt.Errorf("配置项 [netease] proxy 必须为 http/https/socks5/socks5h 代理 URL，主机名不能为空，端口须为 1..65535，不允许路径、query 或 fragment")
	u, err := url.Parse(c.Proxy)
	if err != nil {
		return nil, invalid
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, invalid
	}
	if u.Hostname() == "" || u.Opaque != "" ||
		(u.EscapedPath() != "" && u.EscapedPath() != "/") ||
		u.ForceQuery || u.RawQuery != "" || strings.Contains(c.Proxy, "#") ||
		strings.HasSuffix(u.Host, ":") ||
		(strings.Contains(u.Hostname(), ":") && !strings.HasPrefix(u.Host, "[")) {
		return nil, invalid
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, invalid
		}
	}
	return u, nil
}

// HTTPClient 要求配置已校验；空代理保留默认传输行为
func (c NeteaseConfig) HTTPClient() (*http.Client, error) {
	u, err := c.proxyURL()
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	if u == nil {
		return client, nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(u)
	client.Transport = transport
	return client, nil
}

type DatabaseConfig struct {
	Path string `toml:"path"`
}

type DownloadConfig struct {
	Timeout      Duration `toml:"timeout"`
	Retries      int      `toml:"retries"`
	Concurrency  int      `toml:"concurrency"`
	ReverseProxy string   `toml:"reverse_proxy"`
	AutoRetry    bool     `toml:"auto_retry"`
}

type LogConfig struct {
	Level string `toml:"level"`
	Path  string `toml:"path"`
}

type PluginsConfig struct {
	Dir                  string                    `toml:"dir"`
	Enabled              []string                  `toml:"enabled"`
	Default              string                    `toml:"default"`
	Instances            int                       `toml:"instances"`
	CallTimeout          Duration                  `toml:"call_timeout"`
	MemoryMB             int                       `toml:"memory_mb"`
	HTTPMaxBytes         int64                     `toml:"http_max_bytes"`
	AllowPrivateNetworks bool                      `toml:"allow_private_networks"`
	Config               map[string]map[string]any `toml:"config"`
}

type Config struct {
	Bot      BotConfig      `toml:"bot"`
	Netease  NeteaseConfig  `toml:"netease"`
	Plugins  PluginsConfig  `toml:"plugins"`
	Database DatabaseConfig `toml:"database"`
	Download DownloadConfig `toml:"download"`
	Log      LogConfig      `toml:"log"`
}

func Default() *Config {
	return &Config{
		Bot: BotConfig{
			APIURL:        "https://api.telegram.org",
			UploadTimeout: Duration(120 * time.Second),
		},
		Netease: NeteaseConfig{
			Level: "hires",
		},
		Plugins: PluginsConfig{
			Dir:          "./plugins",
			Instances:    1,
			CallTimeout:  Duration(30 * time.Second),
			MemoryMB:     128,
			HTTPMaxBytes: 8 << 20,
		},
		Database: DatabaseConfig{
			Path: "cache.db",
		},
		Download: DownloadConfig{
			Timeout:     Duration(60 * time.Second),
			Retries:     3,
			Concurrency: 4,
			AutoRetry:   true,
		},
		Log: LogConfig{
			Level: "info",
			Path:  "./log",
		},
	}
}

// Load 严格解析 TOML，并应用默认值及环境变量覆盖
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := Default()
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	// 先合并文件内的 legacy 段，再应用环境变量，确保环境变量优先级最高
	mergeLegacyPluginConfig(cfg)
	if err := applyEnv(cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// mergeLegacyPluginConfig 让 [plugins.config.netease] 覆盖旧 [netease] 段，
// 使宿主的 HTTP 代理/音质校验与传给插件的配置保持一致。
func mergeLegacyPluginConfig(c *Config) {
	values := c.Plugins.Config["netease"]
	if v, ok := values["level"].(string); ok && v != "" {
		c.Netease.Level = v
	}
	// 空字符串不覆盖，避免 [plugins.config.netease] 里未写 proxy 时清掉 [netease] proxy
	if v, ok := values["proxy"].(string); ok && v != "" {
		c.Netease.Proxy = v
	}
}

// Validate 允许 music_u 为空，由启动流程引导登录
func (c *Config) Validate() error {
	if c.Bot.Token == "" {
		return fmt.Errorf("配置项 [bot] token 不能为空")
	}
	if _, err := url.Parse(c.Bot.APIURL); err != nil || c.Bot.APIURL == "" {
		return fmt.Errorf("配置项 [bot] api_url %q 不是合法 URL", c.Bot.APIURL)
	}
	if c.Bot.UploadTimeout <= 0 {
		return fmt.Errorf("配置项 [bot] upload_timeout 必须为正时长")
	}
	switch c.Netease.Level {
	case "standard", "higher", "lossless", "hires", "jyeffect", "sky", "jymaster":
	default:
		return fmt.Errorf("配置项 [netease] level %q 不合法（可选 standard/higher/lossless/hires/jyeffect/sky/jymaster）", c.Netease.Level)
	}
	if _, err := c.Netease.proxyURL(); err != nil {
		return err
	}
	for _, id := range c.Bot.Admin {
		if id <= 0 {
			return fmt.Errorf("配置项 [bot] admin 包含非法 ID %d", id)
		}
	}
	if c.Plugins.Dir == "" {
		return fmt.Errorf("配置项 [plugins] dir 不能为空")
	}
	if c.Plugins.Instances <= 0 {
		return fmt.Errorf("配置项 [plugins] instances 必须为正整数")
	}
	if c.Plugins.CallTimeout <= 0 {
		return fmt.Errorf("配置项 [plugins] call_timeout 必须为正时长")
	}
	if c.Plugins.MemoryMB <= 0 {
		return fmt.Errorf("配置项 [plugins] memory_mb 必须为正整数")
	}
	if c.Plugins.HTTPMaxBytes <= 0 {
		return fmt.Errorf("配置项 [plugins] http_max_bytes 必须为正整数")
	}
	if c.Database.Path == "" {
		return fmt.Errorf("配置项 [database] path 不能为空")
	}
	if c.Download.Timeout <= 0 {
		return fmt.Errorf("配置项 [download] timeout 必须为正时长")
	}
	if c.Download.Retries < 0 {
		return fmt.Errorf("配置项 [download] retries 不能为负数")
	}
	if c.Download.Concurrency <= 0 {
		return fmt.Errorf("配置项 [download] concurrency 必须为正整数")
	}
	if _, err := logrus.ParseLevel(c.Log.Level); err != nil {
		return fmt.Errorf("配置项 [log] level %q 不合法", c.Log.Level)
	}
	if c.Log.Path == "" {
		return fmt.Errorf("配置项 [log] path 不能为空")
	}
	return nil
}

const envPrefix = "TUNEWEAVE_"

// applyEnv 用 TUNEWEAVE_<段>_<键> 环境变量覆盖配置项
func applyEnv(cfg *Config) error {
	if err := walkStruct(reflect.ValueOf(cfg).Elem(), "", func(v reflect.Value, envName string) error {
		s, ok := os.LookupEnv(envName)
		if !ok {
			return nil
		}
		return setFromString(v, s, envName)
	}); err != nil {
		return err
	}
	return applyPluginEnv(cfg)
}

// applyPluginEnv 支持 TUNEWEAVE_PLUGINS_CONFIG_<NAME>_<KEY>（字符串值）。
func applyPluginEnv(cfg *Config) error {
	const prefix = "TUNEWEAVE_PLUGINS_CONFIG_"
	for _, env := range os.Environ() {
		key, value, ok := strings.Cut(env, "=")
		if !ok || !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		i := strings.LastIndexByte(rest, '_')
		if i <= 0 || i == len(rest)-1 {
			continue
		}
		name := strings.ToLower(rest[:i])
		keyName := strings.ToLower(rest[i+1:])
		if cfg.Plugins.Config == nil {
			cfg.Plugins.Config = make(map[string]map[string]any)
		}
		if cfg.Plugins.Config[name] == nil {
			cfg.Plugins.Config[name] = make(map[string]any)
		}
		cfg.Plugins.Config[name][keyName] = value
	}
	return nil
}

func walkStruct(v reflect.Value, path string, fn func(reflect.Value, string) error) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := f.Tag.Get("toml")
		if name == "" {
			continue
		}
		envName := envPrefix + strings.ToUpper(path+name)
		fv := v.Field(i)
		if fv.Kind() == reflect.Map {
			// 插件配置用 TUNEWEAVE_PLUGINS_CONFIG_<NAME>_<KEY> 单独处理
			continue
		}
		// 嵌套配置段递归展开；Duration 的底层是 Int64，不会进入此分支
		if f.Type.Kind() == reflect.Struct {
			if err := walkStruct(fv, strings.ToUpper(name)+"_", fn); err != nil {
				return err
			}
			continue
		}
		if err := fn(fv, envName); err != nil {
			return err
		}
	}
	return nil
}

func setFromString(v reflect.Value, s, name string) error {
	if v.Type() == reflect.TypeOf(Duration(0)) {
		var d Duration
		if err := d.UnmarshalText([]byte(s)); err != nil {
			return fmt.Errorf("环境变量 %s=%q 不是合法时长", name, s)
		}
		v.Set(reflect.ValueOf(d))
		return nil
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("环境变量 %s=%q 不是合法 bool", name, s)
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("环境变量 %s=%q 无法解析为数字", name, s)
		}
		v.SetInt(n)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.String {
			var values []string
			for _, part := range strings.Split(s, ",") {
				if part = strings.TrimSpace(part); part != "" {
					values = append(values, part)
				}
			}
			v.Set(reflect.ValueOf(values))
			return nil
		}
		if v.Type().Elem().Kind() != reflect.Int64 {
			return fmt.Errorf("环境变量 %s 不支持的类型 %s", name, v.Type())
		}
		var ids []int64
		for _, part := range strings.Split(s, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil {
				return fmt.Errorf("环境变量 %s=%q 包含非法数字 %q", name, s, part)
			}
			ids = append(ids, id)
		}
		v.Set(reflect.ValueOf(ids))
	default:
		return fmt.Errorf("环境变量 %s 不支持的类型 %s", name, v.Kind())
	}
	return nil
}
