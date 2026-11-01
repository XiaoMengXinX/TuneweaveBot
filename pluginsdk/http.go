package pluginsdk

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// HTTPRequest 是插件发往宿主的 HTTP 请求。
type HTTPRequest struct {
	Method    string              `json:"method"`
	URL       string              `json:"url"`
	Headers   map[string][]string `json:"headers,omitempty"`
	Body      []byte              `json:"body,omitempty"`
	TimeoutMS int                 `json:"timeout_ms,omitempty"`
}

// HTTPResponse 是宿主返回的 HTTP 响应。4xx/5xx 也是正常响应。
type HTTPResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
}

// NewHTTPClient 返回把请求转发给宿主的 http.Client，可直接交给
// Music163Api-Go / 识曲库使用。
func NewHTTPClient() *http.Client {
	return &http.Client{Transport: hostTransport{}}
}

type hostTransport struct{}

func (hostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// GET 等无请求体的请求 Body 为 nil，io.ReadAll(nil) 会 panic
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	resp, err := hostHTTP(HTTPRequest{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: req.Header,
		Body:    body,
	})
	if err != nil {
		return nil, err
	}
	header := make(http.Header, len(resp.Headers))
	for key, values := range resp.Headers {
		header[key] = append([]string(nil), values...)
	}
	return &http.Response{
		StatusCode: resp.Status,
		Status:     fmt.Sprintf("%d %s", resp.Status, http.StatusText(resp.Status)),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(resp.Body)),
		Request:    req,
	}, nil
}

// hostHTTP 把一次 HTTP 请求转发给宿主代理执行。
func hostHTTP(req HTTPRequest) (*HTTPResponse, error) {
	var resp HTTPResponse
	if err := callHost("http.request", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
