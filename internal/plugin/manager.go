package plugin

import (
	"context"
	"encoding/json"
	"fmt"
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
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if len(enabled) != 0 && !enabled[name] {
			continue
		}
		pluginCfg := cfg.Configs[name]
		if len(pluginCfg) == 0 {
			pluginCfg = json.RawMessage(`{}`)
		}
		p, err := m.runtime.loadPlugin(ctx, path, pluginCfg)
		if err != nil {
			return fmt.Errorf("加载插件 %s 失败: %w", name, err)
		}
		if _, exists := m.plugins[p.manifest.Name]; exists {
			p.Close(ctx)
			return fmt.Errorf("插件名 %q 重复", p.manifest.Name)
		}
		m.plugins[p.manifest.Name] = p
		m.order = append(m.order, p.manifest.Name)
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
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".wasm") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func (m *Manager) Providers() *provider.Registry { return m.registry }

func (m *Manager) Auths() *auth.Manager { return m.auths }

func (m *Manager) Recognizers() []provider.Recognizer { return m.recognizers }

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
	Capabilities []string
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
			Capabilities: append([]string(nil), p.manifest.Capabilities...),
		})
	}
	return out
}

func (m *Manager) Close(ctx context.Context) {
	for _, name := range m.order {
		m.plugins[name].Close(ctx)
	}
	_ = m.runtime.Close(ctx)
}
