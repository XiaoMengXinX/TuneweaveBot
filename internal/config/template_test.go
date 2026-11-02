package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/plugin"
)

func TestGenerateTemplate(t *testing.T) {
	schemas := []plugin.PluginConfigSchema{{
		Name:        "demo",
		DisplayName: "示例",
		Fields: []plugin.ConfigField{
			{Key: "token", Type: "string", Default: "", Secret: true, Comment: "访问凭据"},
			{Key: "region", Type: "string", Default: "cn", Comment: "区域"},
			{Key: "retries", Type: "int", Default: float64(3)},
			{Key: "enabled", Type: "bool", Default: true},
		},
	}}

	out := GenerateTemplate(schemas)

	if !strings.Contains(string(out), "[plugins.demo]") {
		t.Fatal("模板缺少 [plugins.demo] 段")
	}
	if !strings.Contains(string(out), "token = \"\"") {
		t.Error("secret 字段应写空值")
	}
	if !strings.Contains(string(out), "retries = 3") {
		t.Error("int 默认值应渲染为整数")
	}
	if !strings.Contains(string(out), "enabled = true") {
		t.Error("bool 默认值应渲染为 true")
	}

	// 生成的宿主段必须能通过严格模式解析（插件段已被剥离）
	stripped, sections, err := extractPluginSections(out)
	if err != nil {
		t.Fatalf("剥离插件段失败: %v", err)
	}
	cfg := Default()
	dec := toml.NewDecoder(bytes.NewReader(stripped))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		t.Fatalf("宿主段严格解析失败: %v", err)
	}
	if _, ok := sections["demo"]; !ok {
		t.Error("插件段 demo 未被解析")
	}
}

func TestGenerateTemplateFullLoad(t *testing.T) {
	out := GenerateTemplate(nil)
	// 补全必填项后应能完整加载
	filled := strings.Replace(string(out), `token = ""`, `token = "123:abc"`, 1)
	filled = strings.Replace(filled, `default = ""`, `default = "x"`, 1)

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(filled), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("补全后加载失败: %v", err)
	}
}
