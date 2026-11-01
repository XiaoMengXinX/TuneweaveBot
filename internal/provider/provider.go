package provider

import (
	"context"
	"errors"
	"fmt"
)

// 错误哨兵，宿主据此映射用户文案。
var (
	ErrNotFound     = errors.New("not found")
	ErrUnsupported  = errors.New("unsupported")
	ErrAuthRequired = errors.New("authentication required")
	ErrRateLimited  = errors.New("rate limited")
	ErrNoResult     = errors.New("no result")
)

// NoAudioError 表示歌曲存在但当前账号/地区无法获取下载地址。
type NoAudioError struct {
	Code int
	Fee  int
}

func (e *NoAudioError) Error() string {
	return fmt.Sprintf("no audio url (code %d, fee %d)", e.Code, e.Fee)
}

// Provider 是一个音乐平台适配器。
type Provider interface {
	Name() string
	DisplayName() string
	// Code 是用于 Telegram 回调/start 参数的短标识（1-8 位小写字母数字）。
	Code() string
	// Match 从用户输入（链接、ID、provider:id）中识别资源引用。
	Match(ctx context.Context, text string) (*Ref, bool)
	// Canonicalize 把子资源（节目等）归一为可缓存的歌曲 Ref。
	Canonicalize(ctx context.Context, ref Ref) (Ref, error)
	// Resolve 解析歌曲元数据与下载地址；无法获取地址时返回非 nil Track 和 NoAudioError。
	Resolve(ctx context.Context, ref Ref) (*Track, error)
	// PageURL 返回资源在 provider 网页/App 中的链接。
	PageURL(ref Ref) string
	Search(ctx context.Context, keyword string, limit, offset int) ([]TrackSummary, int, error)
	Lyrics(ctx context.Context, ref Ref) (*Lyric, error)
}

// ShareURLProvider 是可选能力：返回 inline 分享查询用的链接。
// 未实现时 Registry 使用 PageURL。
type ShareURLProvider interface {
	ShareURL(ref Ref) string
}

// MetadataProvider 是可选能力：只获取展示所需元数据，不解析下载地址。
// 未实现的 provider 由 Registry 回退到 Resolve。
type MetadataProvider interface {
	Metadata(ctx context.Context, ref Ref) (*TrackSummary, error)
}

// Recognizer 识别音频指纹并返回歌曲引用。
type Recognizer interface {
	// Name 返回所属 provider 名。
	Name() string
	Recognize(ctx context.Context, audio []byte, mime string) (*Ref, error)
}

const (
	AuthWaiting = "waiting"
	AuthScanned = "scanned"
	AuthSuccess = "success"
	AuthExpired = "expired"
)

// AuthStatus 是 provider 当前的登录状态。
type AuthStatus struct {
	Authenticated bool
	Account       string
}

// AuthChallenge 是扫码登录等认证流程的起始信息。
type AuthChallenge struct {
	Type      string // qr
	Content   string // 二维码内容
	Key       string // 轮询 key
	ExpiresIn int    // 秒
}

// Persist 指示宿主把凭据写回配置的哪个键。
type Persist struct {
	Section string
	Key     string
}

// AuthState 是认证轮询的结果。
type AuthState struct {
	State      string
	Credential string
	Nickname   string
	Persist    *Persist
}

// Authenticator 是可选的登录能力。
type Authenticator interface {
	Name() string
	Status(ctx context.Context) (AuthStatus, error)
	Begin(ctx context.Context) (*AuthChallenge, error)
	Poll(ctx context.Context, key string) (*AuthState, error)
	Apply(ctx context.Context, credential string) error
}
