package plugin

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/auth"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

type Config struct {
	Dir     string
	Enabled []string
	Default string
	Runtime RuntimeConfig
	Configs map[string]json.RawMessage
	// Instances 是按插件名覆盖的实例数，未设置的插件使用 Runtime.Instances
	Instances map[string]int
}

// Rollback 记录一次更新后加载失败、已回滚到备份版本的插件。
type Rollback struct {
	Name      string
	Version   string // 回滚后的版本
	BuildTime string
	Err       error // 新版本的加载错误
}

// Manager 管理已加载的插件并提供 provider/auth/recognizer 适配器。
type Manager struct {
	runtime     *Runtime
	plugins     map[string]*Plugin
	order       []string
	defaultName string
	registry    *provider.Registry
	auths       *auth.Manager
	recognizers []provider.Recognizer
	rollbacks   []Rollback
}

// Load 扫描目录、加载插件并装配 registry。
func Load(ctx context.Context, cfg Config, st *store.Store, log *logrus.Logger, httpClient *http.Client) (*Manager, error) {
	runtime, err := NewRuntime(ctx, cfg.Runtime, st, log, httpClient)
	if err != nil {
		return nil, err
	}
	m := &Manager{runtime: runtime, plugins: make(map[string]*Plugin)}
	if err := m.load(ctx, cfg); err != nil {
		_ = runtime.Close(context.Background())
		return nil, err
	}
	return m, nil
}

func (m *Manager) load(ctx context.Context, cfg Config) error {
	files, err := pluginFiles(cfg.Dir)
	if err != nil {
		return err
	}
	enabled := make(map[string]bool, len(cfg.Enabled))
	for _, name := range cfg.Enabled {
		enabled[name] = true
	}
	for _, path := range files {
		name, _ := pluginBaseName(filepath.Base(path))
		if len(enabled) != 0 && !enabled[name] {
			continue
		}
		pluginCfg := cfg.Configs[name]
		if len(pluginCfg) == 0 {
			pluginCfg = json.RawMessage(`{}`)
		}
		instances := cfg.Instances[name]
		if instances <= 0 {
			instances = m.runtime.cfg.Instances
		}
		p, err := m.loadWithRollback(ctx, name, path, pluginCfg, instances)
		if err != nil {
			return fmt.Errorf("加载插件 %s 失败: %w", name, err)
		}
		if _, exists := m.plugins[p.manifest.Name]; exists {
			p.Close(ctx)
			return fmt.Errorf("插件名 %q 重复", p.manifest.Name)
		}
		m.plugins[p.manifest.Name] = p
		m.order = append(m.order, p.manifest.Name)
		build := BuildLabel(p.manifest.Commit, p.manifest.BuildTime)
		if build == "" {
			build = "无构建信息"
		}
		m.runtime.log.Printf("已加载插件 %s v%s (%s) [%s] x%d", p.manifest.Name, p.manifest.Version, build, strings.Join(p.manifest.Capabilities, ", "), cap(p.pool))
	}
	if len(m.order) == 0 {
		return fmt.Errorf("插件目录 %s 中没有可加载的 .wasm 插件", cfg.Dir)
	}

	defaultName := cfg.Default
	if defaultName == "" {
		for _, name := range m.order {
			if m.plugins[name].manifest.has("provider") {
				defaultName = name
				break
			}
		}
	}
	if defaultName == "" {
		return fmt.Errorf("没有可用的 provider 插件")
	}
	if p, ok := m.plugins[defaultName]; !ok || !p.manifest.has("provider") {
		return fmt.Errorf("默认 provider %q 未加载或不支持 provider 能力", defaultName)
	}
	m.defaultName = defaultName

	providers := make([]provider.Provider, 0, len(m.order))
	var authenticators []provider.Authenticator
	for _, name := range m.order {
		p := m.plugins[name]
		if p.manifest.has("provider") {
			providers = append(providers, &ProviderAdapter{plugin: p})
		}
		if p.manifest.has("recognizer") {
			m.recognizers = append(m.recognizers, &RecognizerAdapter{plugin: p})
		}
		if p.manifest.has("auth") {
			authenticators = append(authenticators, &AuthAdapter{plugin: p})
		}
	}
	m.registry, err = provider.NewRegistry(defaultName, providers...)
	if err != nil {
		return err
	}
	m.auths = auth.New(authenticators...)
	return nil
}

func pluginFiles(dir string) ([]string, error) {
	if dir == "" {
		dir = "plugins"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取插件目录 %s 失败: %w", dir, err)
	}
	var files []string
	seen := map[string]string{}
	for _, entry := range entries {
		name, ok := pluginBaseName(entry.Name())
		if entry.IsDir() || !ok {
			continue
		}
		if other, dup := seen[name]; dup {
			return nil, fmt.Errorf("插件目录 %s 中同时存在 %s 与 %s，请只保留一个", dir, other, entry.Name())
		}
		seen[name] = entry.Name()
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

// pluginBaseName 去掉插件文件后缀 .wasm 或 .wasm.gz，其他文件返回 false。
func pluginBaseName(file string) (string, bool) {
	lower := strings.ToLower(file)
	for _, ext := range []string{".wasm.gz", ".wasm"} {
		if strings.HasSuffix(lower, ext) {
			return file[:len(file)-len(ext)], true
		}
	}
	return "", false
}

// readPluginFile 读取插件文件，gzip 压缩的内容在此解压。
func readPluginFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decompressWasm(data)
}

func isGzip(data []byte) bool { return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b }

// decompressWasm 解压 gzip 内容，非 gzip 原样返回；解压后上限 maxPluginWasmBytes。
func decompressWasm(data []byte) ([]byte, error) {
	if !isGzip(data) {
		return data, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解压插件失败: %w", err)
	}
	wasm, err := io.ReadAll(io.LimitReader(zr, maxPluginWasmBytes+1))
	if err != nil {
		return nil, fmt.Errorf("解压插件失败: %w", err)
	}
	if len(wasm) > maxPluginWasmBytes {
		return nil, fmt.Errorf("插件解压后超过 %d 字节上限", maxPluginWasmBytes)
	}
	return wasm, nil
}

func (m *Manager) Providers() *provider.Registry { return m.registry }

func (m *Manager) Auths() *auth.Manager { return m.auths }

func (m *Manager) Recognizers() []provider.Recognizer { return m.recognizers }

// MediaRefPrefix 是 Track.AudioURL 中插件已写入文件的哨兵前缀，需与 pluginsdk 一致。
const MediaRefPrefix = "plugin-media:"

// MediaPath 把 media 哨兵（plugin-media:<token>）解析为已落盘的文件路径。
// token 不合法或文件不存在时返回空串。
func (m *Manager) MediaPath(audioURL string) string {
	if m == nil {
		return ""
	}
	return m.runtime.mediaPath(audioURL)
}

func (r *Runtime) mediaPath(audioURL string) string {
	token, ok := strings.CutPrefix(audioURL, MediaRefPrefix)
	if !ok || !isMediaToken(token) {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(r.cfg.MediaDir, "*-"+token+".*"))
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// isMediaToken 校验 token 是 32 位十六进制，避免 glob 注入。
func isMediaToken(token string) bool {
	if len(token) != 32 {
		return false
	}
	for _, c := range token {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (m *Manager) DefaultName() string { return m.defaultName }

// PluginCommand 是插件声明的命令及其所属插件。
type PluginCommand struct {
	Plugin  string
	Command Command
}

// Commands 按加载顺序返回插件声明的命令。
func (m *Manager) Commands() []PluginCommand {
	var out []PluginCommand
	for _, name := range m.order {
		for _, cmd := range m.plugins[name].manifest.Commands {
			out = append(out, PluginCommand{Plugin: name, Command: cmd})
		}
	}
	return out
}

// PluginInfo 是 /status 展示所需的插件信息。
type PluginInfo struct {
	Name         string
	DisplayName  string
	Version      string
	Commit       string
	BuildTime    string
	Capabilities []string
	Path         string // 加载的插件文件
	Instances    int    // 常驻实例数
	Updatable    bool   // manifest 声明了更新地址
}

// PluginConfigSchema 是一个插件声明的配置项集合，供生成/补全配置使用。
type PluginConfigSchema struct {
	Name        string
	DisplayName string
	Fields      []ConfigField
}

// ConfigSchemas 按加载顺序返回各插件声明的配置项。
func (m *Manager) ConfigSchemas() []PluginConfigSchema {
	out := make([]PluginConfigSchema, 0, len(m.order))
	for _, name := range m.order {
		p := m.plugins[name]
		if len(p.manifest.ConfigSchema) == 0 {
			continue
		}
		out = append(out, PluginConfigSchema{
			Name:        p.manifest.Name,
			DisplayName: p.manifest.DisplayName,
			Fields:      p.manifest.ConfigSchema,
		})
	}
	return out
}

func (m *Manager) Info() []PluginInfo {
	out := make([]PluginInfo, 0, len(m.order))
	for _, name := range m.order {
		p := m.plugins[name]
		out = append(out, PluginInfo{
			Name:         p.manifest.Name,
			DisplayName:  p.manifest.DisplayName,
			Version:      p.manifest.Version,
			Commit:       p.manifest.Commit,
			BuildTime:    p.manifest.BuildTime,
			Capabilities: append([]string(nil), p.manifest.Capabilities...),
			Path:         p.path,
			Instances:    cap(p.pool),
			Updatable:    p.manifest.UpdateURL != "",
		})
	}
	return out
}

// ManifestJSON 返回插件 manifest 的格式化 JSON。
func (m *Manager) ManifestJSON(name string) ([]byte, bool) {
	p, ok := m.plugins[name]
	if !ok {
		return nil, false
	}
	data, err := json.MarshalIndent(p.manifest, "", "  ")
	return data, err == nil
}

// BuildLabel 返回 "commit 前 7 位 构建时间"，未注入构建信息时返回空串。
func BuildLabel(commit, buildTime string) string {
	if len(commit) > 7 {
		commit = commit[:7]
	}
	return strings.TrimSpace(commit + " " + buildTime)
}

// Rollbacks 返回本次加载中因新版本加载失败而回滚的插件。
func (m *Manager) Rollbacks() []Rollback {
	if m == nil {
		return nil
	}
	return m.rollbacks
}

// loadWithRollback 加载插件；失败且存在更新前的备份时，恢复备份再加载一次。
// 新版本加载成功后删除备份。
func (m *Manager) loadWithRollback(ctx context.Context, name, path string, config json.RawMessage, instances int) (*Plugin, error) {
	p, err := m.runtime.loadPlugin(ctx, path, config, instances)
	backup := backupPath(path)
	if err == nil {
		if rmErr := os.Remove(backup); rmErr != nil && !os.IsNotExist(rmErr) {
			m.runtime.log.Warnf("删除插件 %s 的更新备份失败: %v", name, rmErr)
		}
		return p, nil
	}
	if _, statErr := os.Stat(backup); statErr != nil {
		return nil, err
	}
	if renameErr := os.Rename(backup, path); renameErr != nil {
		return nil, errors.Join(err, fmt.Errorf("回滚失败: %w", renameErr))
	}
	m.runtime.log.Errorf("插件 %s 更新后加载失败，已回滚到更新前的版本: %v", name, err)
	p, retryErr := m.runtime.loadPlugin(ctx, path, config, instances)
	if retryErr != nil {
		return nil, errors.Join(err, fmt.Errorf("回滚后仍加载失败: %w", retryErr))
	}
	m.rollbacks = append(m.rollbacks, Rollback{Name: p.manifest.Name, Version: p.manifest.Version, BuildTime: p.manifest.BuildTime, Err: err})
	return p, nil
}

func (m *Manager) Close(ctx context.Context) {
	for _, name := range m.order {
		m.plugins[name].Close(ctx)
	}
	_ = m.runtime.Close(ctx)
}
