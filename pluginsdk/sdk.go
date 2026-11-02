// Package pluginsdk 是 TuneweaveBot WASM 插件的 Go SDK。
//
// 插件作者只需实现一个 Handler 并在 init 中调用 Main（c-shared reactor
// 不会执行 main，init 会在宿主调用 _initialize 时运行）：
//
//	func init() { pluginsdk.Main(handle) }
//	func main() {}
//
// 宿主通过 ABI v1 的 JSON envelope 调用插件；插件通过 Context 访问
// 日志、HTTP、KV、配置等宿主能力。
package pluginsdk

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ABI 是当前插件协议版本。
const ABI = 1

// Request 是宿主调用插件时传入的 envelope。
type Request struct {
	ABI     int             `json:"abi"`
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Error 是插件返回给宿主的错误。
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Response 是插件返回给宿主的 envelope。
type Response struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *Error          `json:"error,omitempty"`
}

// Handler 处理一次宿主调用；返回的数据会被编码为 JSON。
type Handler func(*Context) (any, error)

var handler Handler

// Main 注册插件入口。
func Main(h Handler) { handler = h }

// Handle 是供 wasm 导出层和单测使用的同步入口。
func Handle(input []byte) (out []byte) {
	// 命名返回值配合 defer recover，插件 panic 时也能把错误返回给宿主
	defer func() {
		if r := recover(); r != nil {
			out = marshal(Response{Error: &Error{Code: "internal", Message: fmt.Sprint(r)}})
		}
	}()

	var req Request
	if err := json.Unmarshal(input, &req); err != nil {
		return marshal(Response{Error: &Error{Code: "invalid_request", Message: err.Error()}})
	}
	if req.ABI != ABI {
		return marshal(Response{Error: &Error{Code: "invalid_request", Message: fmt.Sprintf("unsupported abi %d", req.ABI)}})
	}
	if handler == nil {
		return marshal(Response{Error: &Error{Code: "internal", Message: "plugin handler not registered"}})
	}

	data, err := handler(&Context{action: req.Action, payload: req.Payload})
	if err != nil {
		code := "internal"
		var pluginErr *Error
		if errors.As(err, &pluginErr) {
			code = pluginErr.Code
		}
		return marshal(Response{Error: &Error{Code: code, Message: err.Error()}})
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return marshal(Response{Error: &Error{Code: "internal", Message: err.Error()}})
	}
	return marshal(Response{OK: true, Data: raw})
}

func marshal(v Response) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"ok":false,"error":{"code":"internal","message":"encode response"}}`)
	}
	return raw
}

// Fail 构造带错误码的插件错误。
func Fail(code, message string) error { return &Error{Code: code, Message: message} }

// ErrHostUnavailable 表示当前不是 wasm 运行时，宿主能力不可用。
var ErrHostUnavailable = errors.New("pluginsdk: host calls are only available in wasm")

// hostCallFn 由 wasm 构建注册；非 wasm 构建保持 nil，便于纯逻辑单测。
var hostCallFn func(name string, req any, out any) error

// callHost 调用宿主能力并把 data 解码到 out。
func callHost(name string, req, out any) error {
	if hostCallFn == nil {
		return ErrHostUnavailable
	}
	return hostCallFn(name, req, out)
}

// Context 是一次调用的上下文。
type Context struct {
	action  string
	payload json.RawMessage
}

// Action 返回宿主请求的动作名。
func (c *Context) Action() string { return c.action }

// Decode 把 payload 解码到 v。
func (c *Context) Decode(v any) error {
	if len(c.payload) == 0 {
		return nil
	}
	return json.Unmarshal(c.payload, v)
}

// Log 写一条宿主日志。
func (c *Context) Log(level, message string) {
	_ = callHost("log", map[string]string{"level": level, "message": message}, nil)
}

// HTTP 执行一次宿主代理的 HTTP 请求。
func (c *Context) HTTP(req HTTPRequest) (*HTTPResponse, error) {
	return hostHTTP(req)
}

// KVGet 读取插件命名空间中的键。
func (c *Context) KVGet(key string) ([]byte, bool, error) {
	var out struct {
		Value []byte `json:"value"`
		Found bool   `json:"found"`
	}
	if err := callHost("kv.get", map[string]string{"key": key}, &out); err != nil {
		return nil, false, err
	}
	return out.Value, out.Found, nil
}

// KVSet 写入插件命名空间中的键。
func (c *Context) KVSet(key string, value []byte) error {
	return callHost("kv.set", map[string]any{"key": key, "value": value}, nil)
}

// KVDelete 删除键。
func (c *Context) KVDelete(key string) error {
	return callHost("kv.delete", map[string]string{"key": key}, nil)
}

// KVList 列出命名空间中的键。
func (c *Context) KVList() ([]string, error) {
	var out struct {
		Keys []string `json:"keys"`
	}
	if err := callHost("kv.list", struct{}{}, &out); err != nil {
		return nil, err
	}
	return out.Keys, nil
}

// Config 把插件配置解码到 v。
func (c *Context) Config(v any) error {
	var out struct {
		Config json.RawMessage `json:"config"`
	}
	if err := callHost("config.get", struct{}{}, &out); err != nil {
		return err
	}
	if len(out.Config) == 0 {
		return nil
	}
	return json.Unmarshal(out.Config, v)
}

// Now 返回宿主当前时间。
func (c *Context) Now() time.Time {
	var out struct {
		UnixNano int64 `json:"unix_nano"`
	}
	if err := callHost("time.now", struct{}{}, &out); err != nil {
		return time.Now()
	}
	return time.Unix(0, out.UnixNano)
}

// Random 返回 n 字节随机数。
func (c *Context) Random(n int) ([]byte, error) {
	var out struct {
		Data []byte `json:"data"`
	}
	if err := callHost("random", map[string]int{"n": n}, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
