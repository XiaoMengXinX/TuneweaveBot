package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Registry 管理已启用的 provider，并负责默认 provider 与 provider:id 解析。
type Registry struct {
	providers   map[string]Provider
	order       []string
	defaultName string
}

func NewRegistry(defaultName string, providers ...Provider) (*Registry, error) {
	r := &Registry{providers: make(map[string]Provider, len(providers)), defaultName: defaultName}
	for _, p := range providers {
		name := p.Name()
		if name == "" {
			return nil, fmt.Errorf("provider 名称不能为空")
		}
		if _, ok := r.providers[name]; ok {
			return nil, fmt.Errorf("provider %q 重复注册", name)
		}
		r.providers[name] = p
		r.order = append(r.order, name)
	}
	if defaultName == "" && len(r.order) != 0 {
		r.defaultName = r.order[0]
	}
	if _, ok := r.providers[r.defaultName]; !ok {
		return nil, fmt.Errorf("默认 provider %q 未加载", r.defaultName)
	}
	return r, nil
}

func (r *Registry) DefaultName() string { return r.defaultName }

func (r *Registry) Default() Provider { return r.providers[r.defaultName] }

func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// Lookup 把 provider 名或短 code 解析为已启用的 provider。
func (r *Registry) Lookup(nameOrCode string) (Provider, bool) {
	if p, ok := r.providers[nameOrCode]; ok {
		return p, true
	}
	for _, name := range r.order {
		if r.providers[name].Code() == nameOrCode {
			return r.providers[name], true
		}
	}
	return nil, false
}

func (r *Registry) Enabled() []Provider {
	out := make([]Provider, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.providers[name])
	}
	return out
}

// Match 先识别 provider:id，再按默认 provider、注册顺序尝试各 provider 的 Match。
func (r *Registry) Match(ctx context.Context, text string) (*Ref, Provider, bool) {
	if ref, p, ok := r.matchExplicit(text); ok {
		return ref, p, true
	}
	order := make([]string, 0, len(r.order))
	order = append(order, r.defaultName)
	for _, name := range r.order {
		if name != r.defaultName {
			order = append(order, name)
		}
	}
	for _, name := range order {
		p := r.providers[name]
		if !mayMatch(p, text) {
			continue
		}
		ref, ok := p.Match(ctx, text)
		if !ok || ref == nil {
			continue
		}
		ref.Provider = name
		return ref, p, true
	}
	return nil, nil, false
}

// MatchProvider 只在指定 provider 上匹配。
func (r *Registry) MatchProvider(ctx context.Context, name, text string) (*Ref, bool) {
	p, ok := r.providers[name]
	if !ok || !mayMatch(p, text) {
		return nil, false
	}
	ref, ok := p.Match(ctx, text)
	if !ok || ref == nil {
		return nil, false
	}
	ref.Provider = name
	return ref, true
}

// mayMatch 用 LinkHosts 在本地预筛，避免为不相关的链接占用插件实例。
// 不像链接的文本（纯 ID、URI）不预筛。
func mayMatch(p Provider, text string) bool {
	lp, ok := p.(LinkHostsProvider)
	if !ok || !LooksLikeLink(text) {
		return true
	}
	hosts := lp.LinkHosts()
	if len(hosts) == 0 {
		return true
	}
	text = strings.ToLower(text)
	for _, host := range hosts {
		if strings.Contains(text, strings.ToLower(host)) {
			return true
		}
	}
	return false
}

func (r *Registry) matchExplicit(text string) (*Ref, Provider, bool) {
	text = strings.TrimSpace(text)
	name, id, ok := strings.Cut(text, ":")
	if !ok || name == "" || id == "" {
		return nil, nil, false
	}
	// 支持 provider 名或短 code（如 ne:123）
	p, ok := r.Lookup(name)
	if !ok {
		return nil, nil, false
	}
	return &Ref{Provider: p.Name(), ID: id}, p, true
}

func (r *Registry) Canonicalize(ctx context.Context, ref Ref) (Ref, error) {
	p, ok := r.providers[ref.Provider]
	if !ok {
		return Ref{}, fmt.Errorf("%w: provider %q", ErrNotFound, ref.Provider)
	}
	out, err := p.Canonicalize(ctx, ref)
	if err != nil {
		return Ref{}, err
	}
	if out.Provider == "" {
		out.Provider = ref.Provider
	}
	return out, nil
}

func (r *Registry) Resolve(ctx context.Context, ref Ref) (*Track, error) {
	p, ok := r.providers[ref.Provider]
	if !ok {
		return nil, fmt.Errorf("%w: provider %q", ErrNotFound, ref.Provider)
	}
	track, err := p.Resolve(ctx, ref)
	if track != nil && track.Ref.Provider == "" {
		track.Ref.Provider = ref.Provider
	}
	return track, err
}

// ShareURL 返回 inline 分享查询用的链接；provider 未实现 ShareURLProvider 时使用 PageURL。
func (r *Registry) ShareURL(ref Ref) string {
	p, ok := r.providers[ref.Provider]
	if !ok {
		return ref.URL
	}
	if sp, ok := p.(ShareURLProvider); ok {
		return sp.ShareURL(ref)
	}
	return p.PageURL(ref)
}

// Metadata 只获取展示元数据；provider 未实现 MetadataProvider 时回退到 Resolve。
func (r *Registry) Metadata(ctx context.Context, ref Ref) (*TrackSummary, error) {
	p, ok := r.providers[ref.Provider]
	if !ok {
		return nil, fmt.Errorf("%w: provider %q", ErrNotFound, ref.Provider)
	}
	if mp, ok := p.(MetadataProvider); ok {
		summary, err := mp.Metadata(ctx, ref)
		if summary != nil && summary.Ref.Provider == "" {
			summary.Ref.Provider = ref.Provider
		}
		return summary, err
	}
	track, err := p.Resolve(ctx, ref)
	if track == nil {
		return nil, err
	}
	// 无音频（VIP/无版权）不影响展示元数据，仍返回标题等信息
	var noAudio *NoAudioError
	if errors.As(err, &noAudio) {
		err = nil
	}
	return track.Summary(), err
}

func (r *Registry) Search(ctx context.Context, name, keyword string, limit, offset int) ([]TrackSummary, int, error) {
	if name == "" {
		name = r.defaultName
	}
	p, ok := r.providers[name]
	if !ok {
		return nil, 0, fmt.Errorf("%w: provider %q", ErrNotFound, name)
	}
	items, total, err := p.Search(ctx, keyword, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		if items[i].Ref.Provider == "" {
			items[i].Ref.Provider = name
		}
	}
	return items, total, nil
}

func (r *Registry) Lyrics(ctx context.Context, ref Ref) (*Lyric, error) {
	p, ok := r.providers[ref.Provider]
	if !ok {
		return nil, fmt.Errorf("%w: provider %q", ErrNotFound, ref.Provider)
	}
	return p.Lyrics(ctx, ref)
}
