package pluginsdk

import "io"

// MediaSink 把插件产生的音频字节分块交给宿主落盘，供下载/上传流程复用。
// 需要插件 manifest 声明 "media" 能力。解密等数据面重的插件用它把结果写回宿主，
// 避免整轨经 resolve 响应一次性穿过 ABI。
//
// 用法：Open 得到一个 token，Write 追加分块，Close 结束；随后在 Track.AudioURL
// 填入 MediaRef(token)，宿主据此跳过 URL 下载、直接使用已落盘的文件。
type MediaSink struct {
	token  string
	closed bool
}

const mediaRefPrefix = "plugin-media:"

// MediaRef 返回放进 Track.AudioURL 的哨兵值，宿主据此识别插件已写入的文件。
func MediaRef(token string) string { return mediaRefPrefix + token }

// OpenMedia 向宿主申请一个媒体写入会话，ext 为不含点的扩展名（如 "m4a"）。
func (c *Context) OpenMedia(ext string) (*MediaSink, error) {
	var out struct {
		Token string `json:"token"`
	}
	if err := callHost("media.open", map[string]string{"ext": ext}, &out); err != nil {
		return nil, err
	}
	return &MediaSink{token: out.Token}, nil
}

// Token 返回会话标识，用于 MediaRef。
func (m *MediaSink) Token() string { return m.token }

// Write 追加一块字节。单块不宜过大：字节会以 base64 编码穿过 ABI。
func (m *MediaSink) Write(p []byte) (int, error) {
	if m.closed {
		return 0, io.ErrClosedPipe
	}
	if len(p) == 0 {
		return 0, nil
	}
	if err := callHost("media.write", map[string]any{"token": m.token, "chunk": p}, nil); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close 结束写入。重复调用无副作用。
func (m *MediaSink) Close() error {
	if m.closed {
		return nil
	}
	m.closed = true
	return callHost("media.close", map[string]string{"token": m.token}, nil)
}

// Abort 放弃写入并让宿主删除半成品文件。
func (m *MediaSink) Abort() error {
	if m.closed {
		return nil
	}
	m.closed = true
	return callHost("media.abort", map[string]string{"token": m.token}, nil)
}

// DownloadResult 是 DownloadToScratch 的结果。
type DownloadResult struct {
	Path string // 沙箱内路径，可直接 os.Open
	Size int64
}

type DownloadOptions struct {
	Headers map[string]string
	// Title/Album 随下载进度上报，供宿主在状态消息中展示
	Title string
	Album string
}

// DownloadToScratch 由宿主多线程下载 url 到可写挂载目录。文件只在本次调用内有效，
// 调用结束后宿主会删除。需要 manifest 声明 "media" 能力，opts 可为 nil。
func (c *Context) DownloadToScratch(url string, opts *DownloadOptions) (*DownloadResult, error) {
	var out struct {
		Path string `json:"path"`
		Size int64  `json:"size"`
	}
	req := map[string]any{"url": url}
	if opts != nil {
		if len(opts.Headers) > 0 {
			req["headers"] = opts.Headers
		}
		if opts.Title != "" {
			req["title"] = opts.Title
		}
		if opts.Album != "" {
			req["album"] = opts.Album
		}
	}
	if err := callHost("media.download", req, &out); err != nil {
		return nil, err
	}
	return &DownloadResult{Path: out.Path, Size: out.Size}, nil
}
