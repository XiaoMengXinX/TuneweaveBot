package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/tetratelabs/wazero/api"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/fetch"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

// instanceState 是一个插件实例可见的宿主能力。
type instanceState struct {
	plugin       string
	config       json.RawMessage
	kv           *store.Store
	allowedHosts []string
	maxHTTPBytes int64
	httpClient   *http.Client
	log          *logrus.Logger

	mediaCapable bool                  // manifest 是否声明 "media" 能力
	mediaDir     string                // 媒体文件落盘目录
	mediaSinks   map[string]*mediaSink // token -> 打开的写入会话
	scratchDir   string                // media.download 落盘目录（宿主路径）
	downloads    []string              // 本次调用中 media.download 落盘的文件
	downloader   *fetch.Downloader
	allowPrivate bool

	// pending 暂存一次因缓冲区不足未能写回的响应，等待插件扩容后用 host.take 取走。
	// 每个实例同一时刻只有一个在途 host.call，无需加锁。
	pending []byte
}

type hostHTTPRequest struct {
	Method    string              `json:"method"`
	URL       string              `json:"url"`
	Headers   map[string][]string `json:"headers,omitempty"`
	Body      []byte              `json:"body,omitempty"`
	TimeoutMS int                 `json:"timeout_ms,omitempty"`
}

type hostHTTPResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
}

// hostCall 是导入到插件的 host.call 实现。
func (r *Runtime) hostCall(ctx context.Context, m api.Module, namePtr, nameLen, reqPtr, reqLen, respPtr, respCap uint32) int64 {
	rawState, ok := r.states.Load(m.Name())
	if !ok {
		return -1
	}
	state := rawState.(*instanceState)

	nameBytes, ok := m.Memory().Read(namePtr, nameLen)
	if !ok {
		return -1
	}
	name := string(nameBytes)

	// host.take 取走上一次因缓冲区不足暂存的响应，不重新执行动作，
	// 避免 HTTP 等带副作用的调用被重复执行。
	if name == "host.take" {
		data := state.pending
		state.pending = nil
		return writeResponse(m, state, respPtr, respCap, data)
	}

	reqBytes, ok := m.Memory().Read(reqPtr, reqLen)
	if !ok {
		return -1
	}

	data, err := state.dispatch(ctx, name, reqBytes)
	if err != nil {
		data, _ = json.Marshal(response{Error: &pluginError{Code: "internal", Message: err.Error()}})
	}
	return writeResponse(m, state, respPtr, respCap, data)
}

func writeResponse(m api.Module, state *instanceState, respPtr, respCap uint32, data []byte) int64 {
	if uint32(len(data)) > respCap {
		// 缓冲区不足：暂存结果并回传所需长度，等插件扩容后用 host.take 取走
		state.pending = data
		if respCap >= 8 {
			var need [8]byte
			binary.LittleEndian.PutUint64(need[:], uint64(len(data)))
			m.Memory().Write(respPtr, need[:])
		}
		return -2
	}
	state.pending = nil
	if !m.Memory().Write(respPtr, data) {
		return -1
	}
	return int64(len(data))
}

func (s *instanceState) dispatch(ctx context.Context, name string, req []byte) ([]byte, error) {
	switch name {
	case "log":
		var in struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(req, &in); err != nil {
			return nil, err
		}
		s.logf(in.Level, in.Message)
		return okResponse(map[string]any{})

	case "http.request":
		var in hostHTTPRequest
		if err := json.Unmarshal(req, &in); err != nil {
			return nil, err
		}
		out, err := s.httpRequest(ctx, in)
		if err != nil {
			return nil, err
		}
		return okResponse(out)

	case "kv.get":
		var in struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(req, &in); err != nil {
			return nil, err
		}
		value, found, err := s.kv.PluginKVGet(s.plugin, in.Key)
		if err != nil {
			return nil, err
		}
		return okResponse(map[string]any{"value": value, "found": found})

	case "kv.set":
		var in struct {
			Key   string `json:"key"`
			Value []byte `json:"value"`
		}
		if err := json.Unmarshal(req, &in); err != nil {
			return nil, err
		}
		if err := s.kv.PluginKVSet(s.plugin, in.Key, in.Value); err != nil {
			return nil, err
		}
		return okResponse(map[string]any{})

	case "kv.delete":
		var in struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(req, &in); err != nil {
			return nil, err
		}
		if err := s.kv.PluginKVDelete(s.plugin, in.Key); err != nil {
			return nil, err
		}
		return okResponse(map[string]any{})

	case "kv.list":
		keys, err := s.kv.PluginKVList(s.plugin)
		if err != nil {
			return nil, err
		}
		return okResponse(map[string]any{"keys": keys})

	case "config.get":
		config := s.config
		if len(config) == 0 {
			config = json.RawMessage(`{}`)
		}
		return okResponse(map[string]any{"config": config})

	case "time.now":
		return okResponse(map[string]any{"unix_nano": time.Now().UnixNano()})

	case "random":
		var in struct {
			N int `json:"n"`
		}
		if err := json.Unmarshal(req, &in); err != nil {
			return nil, err
		}
		if in.N <= 0 || in.N > 1<<20 {
			return nil, fmt.Errorf("invalid random size %d", in.N)
		}
		buf := make([]byte, in.N)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		return okResponse(map[string]any{"data": buf})

	case "media.open", "media.write", "media.close", "media.abort":
		return s.dispatchMedia(name, req)

	case "media.download":
		var in mediaDownloadRequest
		if err := json.Unmarshal(req, &in); err != nil {
			return nil, err
		}
		out, err := s.mediaDownload(ctx, in)
		if err != nil {
			return nil, err
		}
		return okResponse(out)

	default:
		return nil, fmt.Errorf("unknown host call %q", name)
	}
}

func okResponse(data any) ([]byte, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(response{OK: true, Data: raw})
}

func (s *instanceState) logf(level, message string) {
	entry := s.log.WithField("plugin", s.plugin)
	switch strings.ToLower(level) {
	case "debug":
		entry.Debug(message)
	case "warn", "warning":
		entry.Warn(message)
	case "error":
		entry.Error(message)
	default:
		entry.Info(message)
	}
}

func (s *instanceState) logDebug(format string, args ...any) {
	s.log.WithField("plugin", s.plugin).Debugf(format, args...)
}

// logURL 去掉 query 与 fragment 后用于日志，避免记录 token 等参数。
func logURL(u *url.URL) string {
	return u.Scheme + "://" + u.Host + u.Path
}

func (s *instanceState) httpRequest(ctx context.Context, in hostHTTPRequest) (*hostHTTPResponse, error) {
	u, err := url.Parse(in.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if !hostAllowed(u.Hostname(), s.allowedHosts) {
		return nil, fmt.Errorf("host %q not allowed for plugin %s", u.Hostname(), s.plugin)
	}

	method := in.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, in.URL, bytes.NewReader(in.Body))
	if err != nil {
		return nil, err
	}
	for key, values := range in.Headers {
		if strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") {
			continue
		}
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	client := *s.httpClient
	if in.TimeoutMS > 0 {
		client.Timeout = time.Duration(in.TimeoutMS) * time.Millisecond
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		s.logDebug("HTTP %s %s 失败，耗时 %s: %v", req.Method, logURL(u), time.Since(start).Round(time.Millisecond), err)
		return nil, err
	}
	defer resp.Body.Close()

	limit := s.maxHTTPBytes
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	s.logDebug("HTTP %s %s -> %d，%d 字节，耗时 %s", req.Method, logURL(u), resp.StatusCode, len(body), time.Since(start).Round(time.Millisecond))
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}

	headers := make(map[string][]string, len(resp.Header))
	for key, values := range resp.Header {
		headers[key] = append([]string(nil), values...)
	}
	return &hostHTTPResponse{Status: resp.StatusCode, Headers: headers, Body: body}, nil
}

func hostAllowed(host string, allowed []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	// 空白名单拒绝所有：插件必须显式声明可访问的主机，探测阶段也因此被禁止 HTTP
	if len(allowed) == 0 {
		return false
	}
	for _, pattern := range allowed {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == host {
			return true
		}
		if strings.HasPrefix(pattern, "*.") && strings.HasSuffix(host, pattern[1:]) {
			return true
		}
	}
	return false
}

// privateNetworkDialer 在连接前检查解析后的 IP，防止插件把宿主当作 SSRF 跳板。
func privateNetworkDialer(allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	if allowPrivate {
		return dialer.DialContext
	}
	dialer.Control = func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("invalid address %q", address)
		}
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("private network access denied: %s", ip)
		}
		return nil
	}
	return dialer.DialContext
}
