// Package auth 管理各 provider 的登录状态与可用性。
package auth

import (
	"sync"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
)

// Manager 记录 provider 是否可执行需要登录的操作。
// 没有 Authenticator 的 provider 默认始终可用。
type Manager struct {
	mu        sync.RWMutex
	auths     map[string]provider.Authenticator
	available map[string]bool
	order     []string
}

func New(auths ...provider.Authenticator) *Manager {
	m := &Manager{
		auths:     make(map[string]provider.Authenticator, len(auths)),
		available: make(map[string]bool, len(auths)),
	}
	for _, a := range auths {
		name := a.Name()
		m.auths[name] = a
		m.available[name] = true
		m.order = append(m.order, name)
	}
	return m
}

func (m *Manager) Get(name string) (provider.Authenticator, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.auths[name]
	return a, ok
}

func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string(nil), m.order...)
}

// Available 对没有认证要求的 provider 始终返回 true。
func (m *Manager) Available(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.auths[name]; !ok {
		return true
	}
	return m.available[name]
}

// SetAvailable 更新状态并返回是否发生变化。
func (m *Manager) SetAvailable(name string, ok bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.auths[name]; !exists {
		return false
	}
	changed := m.available[name] != ok
	m.available[name] = ok
	return changed
}
