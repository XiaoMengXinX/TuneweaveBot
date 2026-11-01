// 示例 provider 插件：演示最小可用的 WASM 插件结构，返回固定数据。
//
// 构建：bash build-plugins.sh（PLUGINS="example"）
// 配置：
//
//	[plugins]
//	enabled = ["example"]
//	default = "example"
//
//	[plugins.config.example]
//	audio_url_template = "https://example.com/audio/{id}.mp3"
package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/XiaoMengXinX/TuneweaveBot/pluginsdk"
)

const (
	pluginName  = "example"
	displayName = "示例音乐"
	pluginCode  = "ex"
)

var config struct {
	AudioURLTemplate string `json:"audio_url_template"`
}

func init() { pluginsdk.Main(handle) }

func main() {}

func manifest() map[string]any {
	return map[string]any{
		"name":         pluginName,
		"display_name": displayName,
		"version":      "1.0.0",
		"abi":          pluginsdk.ABI,
		"code":         pluginCode,
		"capabilities": []string{"provider"},
		"commands": []map[string]any{
			{"name": "example", "description": "示例 provider 点歌"},
		},
		"http_allowed_hosts": []string{"example.com"},
	}
}

type ref struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Sub       string `json:"sub,omitempty"`
	URL       string `json:"url,omitempty"`
	PageURL   string `json:"page_url,omitempty"`
	ShareText string `json:"share_text,omitempty"`
}

type track struct {
	ref
	Title        string   `json:"title"`
	Artists      []string `json:"artists"`
	Album        string   `json:"album"`
	DurationMS   int      `json:"duration_ms"`
	AudioURL     string   `json:"audio_url,omitempty"`
	Ext          string   `json:"ext,omitempty"`
	Size         int64    `json:"size,omitempty"`
	ProviderName string   `json:"provider_name,omitempty"`
}

type summary struct {
	ref
	Title      string   `json:"title"`
	Artists    []string `json:"artists"`
	Album      string   `json:"album"`
	DurationMS int      `json:"duration_ms"`
}

func handle(c *pluginsdk.Context) (any, error) {
	switch c.Action() {
	case "manifest":
		return manifest(), nil
	case "init":
		config.AudioURLTemplate = "https://example.com/audio/{id}.mp3"
		return nil, c.Config(&config)
	case "shutdown":
		return nil, nil
	case "provider.match":
		var in struct {
			Text string `json:"text"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, pluginsdk.Fail("invalid_request", err.Error())
		}
		id := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.Text), pluginCode+":"))
		if id == "" || strings.ContainsAny(id, " \t\n") {
			return map[string]any{"ref": nil}, nil
		}
		return map[string]any{"ref": makeRef(id)}, nil
	case "provider.canonicalize":
		var in struct {
			Ref ref `json:"ref"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, pluginsdk.Fail("invalid_request", err.Error())
		}
		return map[string]any{"ref": makeRef(in.Ref.ID)}, nil
	case "provider.resolve":
		var in struct {
			Ref ref `json:"ref"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, pluginsdk.Fail("invalid_request", err.Error())
		}
		if in.Ref.ID == "" {
			return nil, pluginsdk.Fail("not_found", "missing id")
		}
		t := track{
			ref:          makeRef(in.Ref.ID),
			Title:        "示例歌曲 " + in.Ref.ID,
			Artists:      []string{"示例歌手"},
			Album:        "示例专辑",
			DurationMS:   180000,
			AudioURL:     strings.ReplaceAll(config.AudioURLTemplate, "{id}", in.Ref.ID),
			Ext:          "mp3",
			ProviderName: displayName,
		}
		return map[string]any{"track": t}, nil
	case "provider.search":
		var in struct {
			Keyword string `json:"keyword"`
			Limit   int    `json:"limit"`
			Offset  int    `json:"offset"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, pluginsdk.Fail("invalid_request", err.Error())
		}
		if in.Limit <= 0 || in.Limit > 50 {
			in.Limit = 10
		}
		items := make([]summary, 0, in.Limit)
		for i := 0; i < in.Limit; i++ {
			id := strconv.Itoa(in.Offset + i + 1)
			items = append(items, summary{
				ref:        makeRef(id),
				Title:      fmt.Sprintf("%s (%s)", in.Keyword, id),
				Artists:    []string{"示例歌手"},
				Album:      "示例专辑",
				DurationMS: 180000,
			})
		}
		return map[string]any{"items": items, "total": 42}, nil
	case "provider.lyrics":
		var in struct {
			Ref ref `json:"ref"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, pluginsdk.Fail("invalid_request", err.Error())
		}
		return map[string]any{
			"lrc":     "[00:00.00]示例歌词\n[00:05.00]示例音乐 " + in.Ref.ID,
			"title":   "示例歌曲 " + in.Ref.ID,
			"artists": []string{"示例歌手"},
			"album":   "示例专辑",
		}, nil
	}
	return nil, pluginsdk.Fail("unsupported", "unknown action "+c.Action())
}

func makeRef(id string) ref {
	link := "https://example.com/track/" + id
	return ref{Provider: pluginName, ID: id, PageURL: link, ShareText: link}
}
