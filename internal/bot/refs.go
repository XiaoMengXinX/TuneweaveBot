package bot

import (
	"encoding/base64"
	"strings"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
)

// Telegram start 参数仅允许 [A-Za-z0-9_-] 且不超过 64 字节；
// 默认 provider 的数字 ID 保持原样以兼容旧链接。
func (a *App) startParam(ref provider.Ref) string {
	if ref.Provider == a.providers.DefaultName() && ref.Sub == "" && isDigits(ref.ID) && len(ref.ID) <= 64 {
		return ref.ID
	}
	return a.encodeRef(ref)
}

func (a *App) parseStartParam(arg string) (provider.Ref, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return provider.Ref{}, false
	}
	if isDigits(arg) {
		return provider.Ref{Provider: a.providers.DefaultName(), ID: arg}, true
	}
	return a.decodeRef(arg)
}

// callbackRef 生成 callback data，兼容旧的 music <数字 ID>。
func (a *App) callbackRef(ref provider.Ref) string {
	if ref.Provider == a.providers.DefaultName() && ref.Sub == "" && isDigits(ref.ID) {
		if data := "music " + ref.ID; len(data) <= 64 {
			return data
		}
	}
	return "music " + a.encodeRef(ref)
}

// parseRefArg 解析数字 ID 或编码引用；provider:id 形式交由调用方回退到 Match 处理。
func (a *App) parseRefArg(arg string) (provider.Ref, bool) {
	if isDigits(arg) {
		return provider.Ref{Provider: a.providers.DefaultName(), ID: arg}, true
	}
	return a.decodeRef(arg)
}

func (a *App) encodeRef(ref provider.Ref) string {
	code := ref.Provider
	if p, ok := a.providers.Get(ref.Provider); ok {
		code = p.Code()
	}
	payload := ref.ID
	if ref.Sub != "" {
		payload = ref.Sub + ":" + ref.ID
	}
	return code + "_" + base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func (a *App) decodeRef(s string) (provider.Ref, bool) {
	code, payload, ok := strings.Cut(s, "_")
	if !ok {
		return provider.Ref{}, false
	}
	p, ok := a.providers.Lookup(code)
	if !ok {
		return provider.Ref{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || len(raw) == 0 {
		return provider.Ref{}, false
	}
	ref := provider.Ref{Provider: p.Name()}
	if sub, id, found := strings.Cut(string(raw), ":"); found {
		ref.Sub, ref.ID = sub, id
	} else {
		ref.ID = string(raw)
	}
	if ref.ID == "" {
		return provider.Ref{}, false
	}
	return ref, true
}

func (a *App) providerDisplayName(ref provider.Ref) string {
	if p, ok := a.providers.Get(ref.Provider); ok {
		return p.DisplayName()
	}
	return render.DefaultProviderName
}

func (a *App) refLink(ref provider.Ref) string {
	if p, ok := a.providers.Get(ref.Provider); ok {
		if link := p.PageURL(ref); link != "" {
			return link
		}
	}
	return ref.URL
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
