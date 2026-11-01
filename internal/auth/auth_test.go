package auth

import (
	"context"
	"testing"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
)

type fakeAuth struct{ name string }

func (f fakeAuth) Name() string { return f.name }
func (f fakeAuth) Status(context.Context) (provider.AuthStatus, error) {
	return provider.AuthStatus{}, nil
}
func (f fakeAuth) Begin(context.Context) (*provider.AuthChallenge, error) { return nil, nil }
func (f fakeAuth) Poll(context.Context, string) (*provider.AuthState, error) {
	return nil, nil
}
func (f fakeAuth) Apply(context.Context, string) error { return nil }

func TestManager(t *testing.T) {
	m := New(fakeAuth{name: "a"}, fakeAuth{name: "b"})

	if got := m.Names(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Names = %v", got)
	}
	if _, ok := m.Get("a"); !ok {
		t.Error("Get(a) 未命中")
	}
	if _, ok := m.Get("missing"); ok {
		t.Error("Get(missing) 不应命中")
	}

	// 已注册的 provider 默认可用；未注册的 provider 没有认证要求，始终可用
	if !m.Available("a") || !m.Available("missing") {
		t.Error("默认应可用")
	}
	if !m.SetAvailable("a", false) {
		t.Error("状态变化应返回 true")
	}
	if m.Available("a") {
		t.Error("SetAvailable(false) 未生效")
	}
	if m.SetAvailable("a", false) {
		t.Error("状态未变化应返回 false")
	}
	if m.SetAvailable("missing", false) {
		t.Error("未注册 provider 不应被修改")
	}
	if !m.Available("missing") {
		t.Error("未注册 provider 仍应可用")
	}
}
