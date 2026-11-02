package config

import (
	"os"
	"strings"
	"testing"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/plugin"
)

func schemaFixture() []plugin.PluginConfigSchema {
	return []plugin.PluginConfigSchema{{
		Name:        "demo",
		DisplayName: "示例",
		Fields: []plugin.ConfigField{
			{Key: "token", Type: "string", Default: "", Secret: true, Comment: "凭据"},
			{Key: "level", Type: "string", Default: "hires", Comment: "音质"},
		},
	}}
}

func TestMissingFields(t *testing.T) {
	cfg := Default()
	cfg.setPluginValue("demo", "token", "x")

	missing := MissingFields(cfg, schemaFixture())
	if len(missing) != 1 || len(missing[0].Fields) != 1 || missing[0].Fields[0].Key != "level" {
		t.Fatalf("应只缺 level，得到 %+v", missing)
	}

	cfg.setPluginValue("demo", "level", "lossless")
	if got := MissingFields(cfg, schemaFixture()); len(got) != 0 {
		t.Errorf("无缺失时应返回空，得到 %+v", got)
	}
}

func TestCompleteConfigExistingSection(t *testing.T) {
	src := "[plugins.demo]\ntoken = \"abc\"\n\n[log]\nlevel = \"info\"\n"
	path := writeTemp(t, src)

	cfg := Default()
	cfg.setPluginValue("demo", "token", "abc")
	if err := CompleteConfig(path, MissingFields(cfg, schemaFixture())); err != nil {
		t.Fatal(err)
	}

	out := readFile(t, path)
	if !strings.Contains(out, "level = \"hires\"") {
		t.Errorf("应补全 level，得到:\n%s", out)
	}
	// level 必须落在 demo 段内，位于 [log] 之前
	if strings.Index(out, "level = \"hires\"") > strings.Index(out, "[log]") {
		t.Errorf("补全项应在 [plugins.demo] 段内:\n%s", out)
	}
	if !strings.Contains(out, "token = \"abc\"") {
		t.Errorf("原有内容应保留:\n%s", out)
	}
}

func TestCompleteConfigNewSection(t *testing.T) {
	src := "[bot]\ntoken = \"t\"\n"
	path := writeTemp(t, src)

	cfg := Default()
	if err := CompleteConfig(path, MissingFields(cfg, schemaFixture())); err != nil {
		t.Fatal(err)
	}

	out := readFile(t, path)
	if !strings.Contains(out, "[plugins.demo]") {
		t.Errorf("应新建 [plugins.demo] 段:\n%s", out)
	}
	if !strings.Contains(out, "token = \"\"") || !strings.Contains(out, "level = \"hires\"") {
		t.Errorf("应写入全部字段:\n%s", out)
	}
	// 补全后文件应能完整加载
	if _, err := Load(path); err != nil {
		t.Fatalf("补全后加载失败: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
