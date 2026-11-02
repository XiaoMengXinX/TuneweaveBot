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

	"github.com/robfig/cron/v3"
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

// proxyURL 校验代理字符串，返回解析结果；空值表示保留默认传输行为。
func proxyURL(proxy string) (*url.URL, error) {
	if proxy == "" {
		return nil, nil
	}
	invalid := fmt.Errorf("配置项 [plugins] proxy 必须为 http/https/socks5/socks5h 代理 URL，主机名不能为空，端口须为 1..65535，不允许路径、query 或 fragment")
	u, err := url.Parse(proxy)
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
		u.ForceQuery || u.RawQuery != "" || strings.Contains(proxy, "#") ||
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

type DatabaseConfig struct {
	Path string `toml:"path"`
}

type DownloadConfig struct {
	Timeout     Duration `toml:"timeout"`
	Retries     int      `toml:"retries"`
	Concurrency int      `toml:"concurrency"`
}

type LogConfig struct {
	Level string `toml:"level"`
	Path  string `toml:"path"`
}

type PluginsConfig struct {
	Dir                  string   `toml:"dir"`
	Enabled              []string `toml:"enabled"`
	Default              string   `toml:"default"`
	Instances            int      `toml:"instances"`
	CallTimeout          Duration `toml:"call_timeout"`
	MemoryMB             int      `toml:"memory_mb"`
	HTTPMaxBytes         int64    `toml:"http_max_bytes"`
	AllowPrivateNetworks bool     `toml:"allow_private_networks"`
	Proxy                string   `toml:"proxy"`
	FSDir                string   `toml:"fs_dir"`
	UpdateCron           string   `toml:"update_cron"`
	// Config 保存每个插件的独立配置段 [plugins.<name>]，直接传给对应插件。
	Config map[string]map[string]any `toml:"-"`
	// PluginInstances 来自 [plugins.<name>] instances，覆盖全局 instances。
	PluginInstances map[string]int `toml:"-"`
}

// pluginsHostKeys 是 [plugins] 段中宿主自身的字段名；其余子表都视为插件配置段。
var pluginsHostKeys = map[string]bool{
	"dir": true, "enabled": true, "default": true, "instances": true,
	"call_timeout": true, "memory_mb": true, "http_max_bytes": true,
	"allow_private_networks": true, "proxy": true, "fs_dir": true, "update_cron": true,
}

// HTTPClient 按 [plugins] proxy 构造插件与宿主共用的 HTTP 客户端；
// 要求配置已校验，空代理保留默认传输行为。
func (p PluginsConfig) HTTPClient() (*http.Client, error) {
	u, err := proxyURL(p.Proxy)
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

type Config struct {
	Bot      BotConfig      `toml:"bot"`
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
		Plugins: PluginsConfig{
			Dir:          "./plugins",
			Instances:    2,
			CallTimeout:  Duration(30 * time.Second),
			MemoryMB:     128,
			HTTPMaxBytes: 16 << 20,
			UpdateCron:   "0 4 * * *",
		},
		Database: DatabaseConfig{
			Path: "cache.db",
		},
		Download: DownloadConfig{
			Timeout:     Duration(60 * time.Second),
			Retries:     3,
			Concurrency: 4,
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
	// 先把每个插件的独立段 [plugins.<name>] 从 [plugins] 中剥离，
	// 使宿主字段仍能走严格模式校验拼写，而子表交给插件按需解释。
	stripped, sections, err := extractPluginSections(data)
	if err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	cfg := Default()
	dec := toml.NewDecoder(bytes.NewReader(stripped))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	cfg.Plugins.Config = sections
	if cfg.Plugins.PluginInstances, err = splitPluginInstances(sections); err != nil {
		return nil, err
	}
	if err := applyEnv(cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// extractPluginSections 解析整份文档，收集 [plugins] 下的插件子表 [plugins.<name>]，
// 返回剔除了这些子表的 TOML（供严格解码只校验 [plugins] 宿主字段）与插件配置段。
func extractPluginSections(data []byte) ([]byte, map[string]map[string]any, error) {
	var root map[string]any
	if err := toml.Unmarshal(data, &root); err != nil {
		return nil, nil, err
	}
	plugins, ok := root["plugins"].(map[string]any)
	if !ok {
		return data, nil, nil
	}
	sections := make(map[string]map[string]any)
	for name, raw := range plugins {
		if pluginsHostKeys[name] {
			continue
		}
		sub, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("配置段 [plugins.%s] 必须为配置表", name)
		}
		// 从严格解码的输入中移除插件子表，避免被 DisallowUnknownFields 拒绝
		delete(plugins, name)
		sections[name] = sub
	}
	if len(sections) == 0 {
		return data, nil, nil
	}
	stripped, err := toml.Marshal(root)
	if err != nil {
		return nil, nil, err
	}
	return stripped, sections, nil
}

// splitPluginInstances 取出各插件段中由宿主解释的 instances，不传给插件。
func splitPluginInstances(sections map[string]map[string]any) (map[string]int, error) {
	out := make(map[string]int)
	for name, section := range sections {
		raw, ok := section["instances"]
		if !ok {
			continue
		}
		n, ok := raw.(int64)
		if !ok || n <= 0 {
			return nil, fmt.Errorf("配置项 [plugins.%s] instances 必须为正整数", name)
		}
		out[name] = int(n)
		delete(section, "instances")
	}
	return out, nil
}

// setPluginValue 写入 [plugins.<name>] 的某个键，必要时创建段。
func (c *Config) setPluginValue(name, key string, value any) {
	if c.Plugins.Config == nil {
		c.Plugins.Config = make(map[string]map[string]any)
	}
	if c.Plugins.Config[name] == nil {
		c.Plugins.Config[name] = make(map[string]any)
	}
	c.Plugins.Config[name][key] = value
}

// Validate 校验宿主配置项；插件凭据由各插件自行处理，可留空后用 /login 登录。
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
	if _, err := proxyURL(c.Plugins.Proxy); err != nil {
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
	if c.Plugins.UpdateCron != "" {
		if _, err := cron.ParseStandard(c.Plugins.UpdateCron); err != nil {
			return fmt.Errorf("配置项 [plugins] update_cron 不合法: %w", err)
		}
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
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("配置项 [log] level %q 不合法，可选 debug/info/warn/error", c.Log.Level)
	}
	if c.Log.Path == "" {
		return fmt.Errorf("配置项 [log] path 不能为空")
	}
	return nil
}

const envPrefix = "TUNEWEAVE_"

// applyEnv 用 TUNEWEAVE_<段>_<键> 环境变量覆盖配置项
func applyEnv(cfg *Config) error {
	return walkStruct(reflect.ValueOf(cfg).Elem(), "", func(v reflect.Value, envName string) error {
		s, ok := os.LookupEnv(envName)
		if !ok {
			return nil
		}
		return setFromString(v, s, envName)
	})
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
			// 插件配置段 [plugins.<name>] 不支持环境变量覆盖
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
