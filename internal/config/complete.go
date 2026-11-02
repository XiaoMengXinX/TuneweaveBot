package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/plugin"
)

// MissingFields 返回各插件声明了、但 [plugins.<name>] 段中缺失的配置项，
// 按插件加载顺序排列，供启动时补全配置使用。
func MissingFields(cfg *Config, schemas []plugin.PluginConfigSchema) []plugin.PluginConfigSchema {
	var out []plugin.PluginConfigSchema
	for _, s := range schemas {
		present := cfg.Plugins.Config[s.Name]
		var missing []plugin.ConfigField
		for _, f := range s.Fields {
			if _, ok := present[f.Key]; !ok {
				missing = append(missing, f)
			}
		}
		if len(missing) > 0 {
			out = append(out, plugin.PluginConfigSchema{Name: s.Name, DisplayName: s.DisplayName, Fields: missing})
		}
	}
	return out
}

// CompleteConfig 把缺失的插件配置项写回配置文件：已有 [plugins.<name>] 段则在段尾追加，
// 否则在文件末尾新建段。保留原有内容与注释，原子写入。
func CompleteConfig(path string, missing []plugin.PluginConfigSchema) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	for _, s := range missing {
		lines = insertFields(lines, s)
	}
	return atomicWrite(path, []byte(strings.Join(lines, "\n")))
}

// insertFields 把一个插件缺失的配置项插入到对应 [plugins.<name>] 段尾，段不存在时新建。
func insertFields(lines []string, s plugin.PluginConfigSchema) []string {
	header := fmt.Sprintf("[plugins.%s]", s.Name)
	start := -1
	for i, line := range lines {
		if m := sectionLine.FindStringSubmatch(line); m != nil && m[1] == "plugins."+s.Name {
			start = i
			break
		}
	}

	var rendered []string
	for _, f := range s.Fields {
		rendered = append(rendered, renderField(f))
	}

	if start < 0 {
		block := []string{""}
		if s.DisplayName != "" {
			block = append(block, "# "+s.DisplayName)
		}
		block = append(block, header)
		block = append(block, rendered...)
		return append(lines, block...)
	}

	// 找到段内最后一个非空行，紧随其后插入
	end := start
	for i := start + 1; i < len(lines); i++ {
		if sectionLine.MatchString(lines[i]) {
			break
		}
		if strings.TrimSpace(lines[i]) != "" {
			end = i
		}
	}
	out := make([]string, 0, len(lines)+len(rendered))
	out = append(out, lines[:end+1]...)
	out = append(out, rendered...)
	out = append(out, lines[end+1:]...)
	return out
}

// renderField 按 template.go 的写法渲染一个配置项行。
func renderField(f plugin.ConfigField) string {
	var b strings.Builder
	writeField(&b, f)
	return strings.TrimRight(b.String(), "\n")
}

// atomicWrite 先写临时文件再 rename，避免写入中断留下半截配置。
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
