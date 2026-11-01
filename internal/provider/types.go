// Package provider 定义与音乐平台无关的领域模型与接口。
package provider

import (
	"encoding/json"
	"strings"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/fetch"
)

// Ref 是 provider 内的资源引用，也是缓存键。
// Sub 用于区分节目、专辑等子资源，Canonicalize 后会归一为可缓存的歌曲 Ref。
type Ref struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Sub      string `json:"sub,omitempty"`
	URL      string `json:"url,omitempty"`
	// PageURL / ShareText 允许插件在纯函数接口中提供链接，避免额外 RPC。
	// Track 自身的同名字段优先。
	PageURL   string `json:"page_url,omitempty"`
	ShareText string `json:"share_text,omitempty"`
}

// Key 返回用于日志/回调的稳定标识。
func (r Ref) Key() string { return r.Provider + ":" + r.ID }

// Track 是解析后的可下载歌曲。
type Track struct {
	Ref
	Title        string            `json:"title"`
	Artists      []string          `json:"artists"`
	Album        string            `json:"album"`
	DurationMS   int               `json:"duration_ms"`
	CoverURL     string            `json:"cover_url,omitempty"`
	AudioURL     string            `json:"audio_url,omitempty"`
	Ext          string            `json:"ext,omitempty"`
	Size         int64             `json:"size,omitempty"`
	BitRate      int               `json:"bit_rate,omitempty"`
	MD5          string            `json:"md5,omitempty"`
	PageURL      string            `json:"page_url,omitempty"`
	ShareText    string            `json:"share_text,omitempty"`
	ProviderName string            `json:"provider_name,omitempty"`
	TagFormat    string            `json:"tag_format,omitempty"`
	TagPayload   json.RawMessage   `json:"tag_payload,omitempty"`
	Download     fetch.Policy      `json:"download,omitempty"`
	Extra        map[string]string `json:"extra,omitempty"`
}

// TrackSummary 是搜索结果中的轻量歌曲信息。
type TrackSummary struct {
	Ref
	Title      string   `json:"title"`
	Artists    []string `json:"artists"`
	Album      string   `json:"album"`
	DurationMS int      `json:"duration_ms"`
}

// Lyric 是一次歌词查询返回的歌词与文件名所需元数据。
type Lyric struct {
	LRC     string   `json:"lrc"`
	Title   string   `json:"title"`
	Artists []string `json:"artists"`
	Album   string   `json:"album"`
}

// LooksLikeLink 判断一段文本是否可能是链接，用于避免把普通数字消息当成歌曲 ID。
func LooksLikeLink(text string) bool {
	return strings.Contains(text, "://") || strings.Contains(text, ".")
}
