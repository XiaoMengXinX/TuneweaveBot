package plugin

import (
	"context"
	"encoding/base64"
	"errors"
	"os"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
)

// ProviderAdapter 把插件包装成 provider.Provider。
type ProviderAdapter struct {
	plugin *Plugin
}

func (a *ProviderAdapter) Name() string        { return a.plugin.manifest.Name }
func (a *ProviderAdapter) DisplayName() string { return a.plugin.manifest.DisplayName }
func (a *ProviderAdapter) Code() string        { return a.plugin.manifest.Code }
func (a *ProviderAdapter) LinkHosts() []string { return a.plugin.manifest.LinkHosts }

func (a *ProviderAdapter) Match(ctx context.Context, text string) (*provider.Ref, bool) {
	var out struct {
		Ref *provider.Ref `json:"ref"`
	}
	if err := a.plugin.call(ctx, "provider.match", map[string]string{"text": text}, &out); err != nil {
		// 匹配失败与出错需要区分：出错时记录日志，否则会被当成"不是链接"而无从排查
		a.plugin.runtime.log.WithField("plugin", a.plugin.manifest.Name).Warnf("provider.match 失败: %v", err)
		return nil, false
	}
	if out.Ref == nil {
		return nil, false
	}
	return out.Ref, true
}

func (a *ProviderAdapter) Canonicalize(ctx context.Context, ref provider.Ref) (provider.Ref, error) {
	var out struct {
		Ref provider.Ref `json:"ref"`
	}
	if err := a.plugin.call(ctx, "provider.canonicalize", map[string]any{"ref": ref}, &out); err != nil {
		return provider.Ref{}, err
	}
	return out.Ref, nil
}

func (a *ProviderAdapter) Resolve(ctx context.Context, ref provider.Ref) (*provider.Track, error) {
	var out struct {
		Track   *provider.Track `json:"track"`
		NoAudio *struct {
			Code int `json:"code"`
			Fee  int `json:"fee"`
		} `json:"no_audio"`
	}
	if err := a.plugin.call(ctx, "provider.resolve", map[string]any{"ref": ref}, &out); err != nil {
		return out.Track, err
	}
	if out.NoAudio != nil {
		return out.Track, &provider.NoAudioError{Code: out.NoAudio.Code, Fee: out.NoAudio.Fee}
	}
	return out.Track, nil
}

// Metadata 只取展示元数据。插件未实现 provider.metadata 时回退到 resolve，
// 并删掉 resolve 顺带落盘的媒体文件。
func (a *ProviderAdapter) Metadata(ctx context.Context, ref provider.Ref) (*provider.TrackSummary, error) {
	var out struct {
		Track *provider.TrackSummary `json:"track"`
	}
	err := a.plugin.call(ctx, "provider.metadata", map[string]any{"ref": ref}, &out)
	if err == nil {
		if out.Track == nil {
			return nil, provider.ErrNoResult
		}
		return out.Track, nil
	}
	if !errors.Is(err, provider.ErrUnsupported) {
		return nil, err
	}

	track, err := a.Resolve(ctx, ref)
	if track == nil {
		return nil, err
	}
	if path := a.plugin.runtime.mediaPath(track.AudioURL); path != "" {
		_ = os.Remove(path)
	}
	// 无音频（VIP/无版权）不影响展示元数据
	var noAudio *provider.NoAudioError
	if errors.As(err, &noAudio) {
		err = nil
	}
	return track.Summary(), err
}

func (a *ProviderAdapter) PageURL(ref provider.Ref) string {
	if ref.PageURL != "" {
		return ref.PageURL
	}
	return ref.URL
}

func (a *ProviderAdapter) ShareURL(ref provider.Ref) string {
	if ref.ShareText != "" {
		return ref.ShareText
	}
	return a.PageURL(ref)
}

func (a *ProviderAdapter) Search(ctx context.Context, keyword string, limit, offset int) ([]provider.TrackSummary, int, error) {
	var out struct {
		Items []provider.TrackSummary `json:"items"`
		Total int                     `json:"total"`
	}
	err := a.plugin.call(ctx, "provider.search", map[string]any{
		"keyword": keyword, "limit": limit, "offset": offset,
	}, &out)
	if err != nil {
		return nil, 0, err
	}
	return out.Items, out.Total, nil
}

func (a *ProviderAdapter) Lyrics(ctx context.Context, ref provider.Ref) (*provider.Lyric, error) {
	var out provider.Lyric
	if err := a.plugin.call(ctx, "provider.lyrics", map[string]any{"ref": ref}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RecognizerAdapter 把插件包装成 provider.Recognizer。
type RecognizerAdapter struct {
	plugin *Plugin
}

func (a *RecognizerAdapter) Name() string { return a.plugin.manifest.Name }

func (a *RecognizerAdapter) Recognize(ctx context.Context, audio []byte, mime string) (*provider.Ref, error) {
	var out struct {
		Ref *provider.Ref `json:"ref"`
	}
	err := a.plugin.call(ctx, "recognizer.recognize", map[string]string{
		"audio": base64.StdEncoding.EncodeToString(audio),
		"mime":  mime,
	}, &out)
	if err != nil {
		return nil, err
	}
	if out.Ref == nil {
		return nil, provider.ErrNoResult
	}
	if out.Ref.Provider == "" {
		out.Ref.Provider = a.Name()
	}
	return out.Ref, nil
}

// AuthAdapter 把插件包装成 provider.Authenticator。
type AuthAdapter struct {
	plugin *Plugin
}

func (a *AuthAdapter) Name() string { return a.plugin.manifest.Name }

func (a *AuthAdapter) Status(ctx context.Context) (provider.AuthStatus, error) {
	var out provider.AuthStatus
	err := a.plugin.call(ctx, "auth.status", struct{}{}, &out)
	return out, err
}

func (a *AuthAdapter) Begin(ctx context.Context, mode string) (*provider.AuthChallenge, error) {
	var out provider.AuthChallenge
	if err := a.plugin.call(ctx, "auth.begin", map[string]string{"mode": mode}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *AuthAdapter) Poll(ctx context.Context, key string) (*provider.AuthState, error) {
	var out provider.AuthState
	if err := a.plugin.call(ctx, "auth.poll", map[string]string{"key": key}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *AuthAdapter) Submit(ctx context.Context, key, token string) (*provider.AuthState, error) {
	var out provider.AuthState
	if err := a.plugin.call(ctx, "auth.submit", map[string]string{"key": key, "token": token}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *AuthAdapter) Apply(ctx context.Context, credential string) error {
	// 广播到实例池并记录新凭据，避免其它实例及重建实例继续使用旧凭据
	return a.plugin.broadcastCredential(ctx, credential)
}
