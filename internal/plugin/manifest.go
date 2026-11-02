package plugin

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ABI 是宿主支持的插件协议版本。
const ABI = 1

// Manifest 是插件通过 manifest action 声明的能力。
type Manifest struct {
	Name             string        `json:"name"`
	DisplayName      string        `json:"display_name"`
	Version          string        `json:"version"`
	ABI              int           `json:"abi"`
	Code             string        `json:"code"`
	Capabilities     []string      `json:"capabilities"`
	AuthRequired     bool          `json:"auth_required"`
	DefaultTagFormat string        `json:"default_tag_format"`
	Commands         []Command     `json:"commands"`
	Resources        Resources     `json:"resources"`
	HTTPAllowedHosts []string      `json:"http_allowed_hosts"`
	LinkHosts        []string      `json:"link_hosts,omitempty"` // provider.match 能识别的链接域名，供宿主预筛
	ConfigSchema     []ConfigField `json:"config_schema,omitempty"`
}

// ConfigField 声明插件 [plugins.<name>] 段的一个配置项，供宿主生成/补全配置。
type ConfigField struct {
	Key     string `json:"key"`
	Type    string `json:"type"` // string | int | bool
	Default any    `json:"default,omitempty"`
	Comment string `json:"comment,omitempty"`
	Secret  bool   `json:"secret,omitempty"`
}

// Command 是插件声明的 Telegram 命令。
// Type 为 "provider"（默认）时把参数交给该插件解析；为 "template" 时
// 先用 Template 生成链接（{args} 会被替换），再交给该插件解析。
type Command struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Usage       string `json:"usage,omitempty"`
	Type        string `json:"type,omitempty"`
	Template    string `json:"template,omitempty"`
}

type Resources struct {
	MemoryMB  int `json:"memory_mb,omitempty"`
	Instances int `json:"instances,omitempty"`
}

const maxPluginCommands = 100

var (
	nameRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	codeRe     = regexp.MustCompile(`^[a-z0-9]{1,8}$`)
	commandRe  = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
	cfgFieldRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_]*$`)
)

func (m *Manifest) validate() error {
	if !nameRe.MatchString(m.Name) {
		return fmt.Errorf("插件名 %q 不合法", m.Name)
	}
	if m.DisplayName == "" {
		return fmt.Errorf("插件 %s 缺少 display_name", m.Name)
	}
	if m.ABI != ABI {
		return fmt.Errorf("插件 %s 的 ABI %d 不受支持（需要 %d）", m.Name, m.ABI, ABI)
	}
	if !codeRe.MatchString(m.Code) {
		return fmt.Errorf("插件 %s 的 code %q 不合法", m.Name, m.Code)
	}
	for _, cap := range m.Capabilities {
		switch cap {
		case "provider", "recognizer", "auth", "media":
		default:
			return fmt.Errorf("插件 %s 声明了未知能力 %q", m.Name, cap)
		}
	}
	if len(m.Commands) > maxPluginCommands {
		return fmt.Errorf("插件 %s 声明的命令过多（最多 %d 条）", m.Name, maxPluginCommands)
	}
	for _, cmd := range m.Commands {
		if !commandRe.MatchString(cmd.Name) {
			return fmt.Errorf("插件 %s 的命令名 %q 不合法", m.Name, cmd.Name)
		}
		if n := len([]rune(cmd.Description)); n < 3 || n > 256 {
			return fmt.Errorf("插件 %s 的命令 %q 描述长度 %d 不合法（需要 3-256 字符）", m.Name, cmd.Name, n)
		}
		switch cmd.Type {
		case "", "provider":
		case "template":
			if !strings.Contains(cmd.Template, "{args}") {
				return fmt.Errorf("插件 %s 的命令 %q 模板缺少 {args}", m.Name, cmd.Name)
			}
		default:
			return fmt.Errorf("插件 %s 的命令 %q 类型 %q 不受支持", m.Name, cmd.Name, cmd.Type)
		}
	}
	seenKeys := make(map[string]bool, len(m.ConfigSchema))
	for _, f := range m.ConfigSchema {
		if !cfgFieldRe.MatchString(f.Key) {
			return fmt.Errorf("插件 %s 的配置项键名 %q 不合法", m.Name, f.Key)
		}
		if seenKeys[f.Key] {
			return fmt.Errorf("插件 %s 的配置项 %q 重复", m.Name, f.Key)
		}
		seenKeys[f.Key] = true
		if err := validateFieldType(f); err != nil {
			return fmt.Errorf("插件 %s 的配置项 %q: %w", m.Name, f.Key, err)
		}
	}
	return nil
}

// validateFieldType 校验 type 合法，且 default（若有）与 type 自洽。
// JSON 把 any 数字解成 float64，int 字段因此接受整值 float64。
func validateFieldType(f ConfigField) error {
	switch f.Type {
	case "string":
		if _, ok := f.Default.(string); f.Default != nil && !ok {
			return fmt.Errorf("default 应为字符串")
		}
	case "bool":
		if _, ok := f.Default.(bool); f.Default != nil && !ok {
			return fmt.Errorf("default 应为布尔值")
		}
	case "int":
		if f.Default == nil {
			return nil
		}
		n, ok := f.Default.(float64)
		if !ok || n != float64(int64(n)) {
			return fmt.Errorf("default 应为整数")
		}
	default:
		return fmt.Errorf("不支持的类型 %q", f.Type)
	}
	return nil
}

func (c Command) CommandType() string {
	if c.Type == "" {
		return "provider"
	}
	return c.Type
}

func (m *Manifest) has(capability string) bool {
	return slices.Contains(m.Capabilities, capability)
}
