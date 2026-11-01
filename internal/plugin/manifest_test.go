package plugin

import (
	"fmt"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		Name:         "demo",
		DisplayName:  "Demo",
		Version:      "1.0.0",
		ABI:          ABI,
		Code:         "dm",
		Capabilities: []string{"provider"},
	}
}

func TestManifestValidate(t *testing.T) {
	m := validManifest()
	if err := m.validate(); err != nil {
		t.Fatalf("valid manifest: %v", err)
	}

	for _, tt := range []struct {
		name   string
		modify func(*Manifest)
	}{
		{"bad name", func(m *Manifest) { m.Name = "Bad Name" }},
		{"bad code", func(m *Manifest) { m.Code = "TOOLONGCODE" }},
		{"bad abi", func(m *Manifest) { m.ABI = 2 }},
		{"bad capability", func(m *Manifest) { m.Capabilities = []string{"web"} }},
		{"bad command name", func(m *Manifest) { m.Commands = []Command{{Name: "Bad", Description: "示例描述"}} }},
		{"short description", func(m *Manifest) { m.Commands = []Command{{Name: "ok", Description: "ab"}} }},
		{"bad command type", func(m *Manifest) { m.Commands = []Command{{Name: "ok", Description: "示例描述", Type: "shell"}} }},
		{"template without args", func(m *Manifest) {
			m.Commands = []Command{{Name: "ok", Description: "示例描述", Type: "template", Template: "https://example.com/"}}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := validManifest()
			tt.modify(&m)
			if err := m.validate(); err == nil {
				t.Error("expected validation error")
			}
		})
	}

	m = validManifest()
	m.Commands = []Command{{Name: "demo", Description: "示例描述", Type: "template", Template: "https://example.com/?id={args}"}}
	if err := m.validate(); err != nil {
		t.Errorf("valid template command: %v", err)
	}
	if got := m.Commands[0].CommandType(); got != "template" {
		t.Errorf("CommandType = %q", got)
	}

	m = validManifest()
	for i := range maxPluginCommands + 1 {
		m.Commands = append(m.Commands, Command{Name: fmt.Sprintf("cmd%d", i), Description: "示例描述"})
	}
	if err := m.validate(); err == nil {
		t.Error("too many commands should fail")
	}
}
