package plugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/fetch"
)

// DownloadProgress 是宿主下载器回传给上层（如 Telegram 状态消息）的进度。
type DownloadProgress struct {
	Written int64  // 已下载字节
	Total   int64  // 文件总大小，未知时为 0
	Speed   string // 最近采样速度
	Title   string // 歌曲名，插件下载前已知时随进度上报，供状态消息展示
	Album   string // 专辑名，同上
}

type progressCtxKey struct{}

// WithDownloadProgress 把进度回调挂到 ctx 上，media.download 执行期间约每 500ms
// 同步调用一次，节流由调用方负责。
func WithDownloadProgress(ctx context.Context, fn func(DownloadProgress)) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, progressCtxKey{}, fn)
}

func downloadProgressFrom(ctx context.Context) func(DownloadProgress) {
	fn, _ := ctx.Value(progressCtxKey{}).(func(DownloadProgress))
	return fn
}

// mediaDownload 把 URL 下载到 scratch 目录，返回沙箱内路径与文件大小。
// 文件只在本次插件调用内有效，调用结束时由 releaseDownloads 删除。
func (s *instanceState) mediaDownload(ctx context.Context, in mediaDownloadRequest) (*mediaDownloadResponse, error) {
	if !s.mediaCapable {
		return nil, fmt.Errorf("plugin %s lacks media capability", s.plugin)
	}
	u, err := url.Parse(in.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if !hostAllowed(u.Hostname(), s.allowedHosts) {
		return nil, fmt.Errorf("host %q not allowed for plugin %s", u.Hostname(), s.plugin)
	}
	if err := fetch.ValidateURL(in.URL, s.allowPrivate); err != nil {
		return nil, err
	}
	start := time.Now()
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	fileName := s.plugin + "-dl-" + hex.EncodeToString(raw[:]) + ".bin"
	hostPath := filepath.Join(s.scratchDir, fileName)
	s.downloads = append(s.downloads, hostPath)

	// 下载器不支持限长，进度里一旦看到超限的总大小就取消
	dlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var tooLarge bool
	progress := downloadProgressFrom(ctx)
	onProgress := func(p fetch.Progress) {
		if p.Total > maxMediaBytes || p.Written > maxMediaBytes {
			tooLarge = true
			cancel()
			return
		}
		if progress != nil {
			progress(DownloadProgress{
				Written: p.Written,
				Total:   p.Total,
				Speed:   p.Speed,
				Title:   in.Title,
				Album:   in.Album,
			})
		}
	}

	req := fetch.Request{
		URL:      in.URL,
		FileName: fileName,
		Headers:  in.Headers,
		Policy:   fetch.Policy{ForceMultiThread: true},
	}
	tooLargeErr := fmt.Errorf("media download exceeds %d bytes", maxMediaBytes)
	if err := s.downloader.DownloadMusic(dlCtx, req, onProgress); err != nil {
		if tooLarge {
			return nil, tooLargeErr
		}
		return nil, err
	}

	info, err := os.Stat(hostPath)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxMediaBytes {
		return nil, tooLargeErr
	}
	s.logDebug("下载 %s 完成，%d 字节，耗时 %s", logURL(u), info.Size(), time.Since(start).Round(time.Millisecond))
	return &mediaDownloadResponse{
		Path: ScratchMount + "/" + fileName,
		Size: info.Size(),
	}, nil
}

// releaseDownloads 删除本次调用中 media.download 落盘的文件。
func (s *instanceState) releaseDownloads() {
	for _, path := range s.downloads {
		_ = os.Remove(path)
	}
	s.downloads = s.downloads[:0]
}

type mediaDownloadRequest struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Title   string            `json:"title,omitempty"`
	Album   string            `json:"album,omitempty"`
}

type mediaDownloadResponse struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}
