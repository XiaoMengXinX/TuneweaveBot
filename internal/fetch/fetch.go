// Package fetch 下载音乐与文件
package fetch

import (
	"context"
	"strings"
	"time"

	downloader "github.com/XiaoMengXinX/SimpleDownloader"
)

// Policy 是 provider 提供的下载策略，零值表示使用下载器默认行为。
type Policy struct {
	HostReplace      map[string]string `json:"host_replace,omitempty"`
	ForceHTTPS       bool              `json:"force_https,omitempty"`
	ForceMultiThread bool              `json:"force_multi_thread,omitempty"`
}

// Request 是一次歌曲下载请求。
type Request struct {
	URL      string
	FileName string
	Headers  map[string]string
	Policy   Policy
}

type Progress struct {
	Attempt int    // 第几次尝试，从 1 开始
	Written int64  // 已下载字节
	Total   int64  // 文件总大小
	Speed   string // 最近 500ms 的速度
}

type Downloader struct {
	d       *downloader.Downloader
	retries int
}

func New(savePath string, timeout time.Duration, retries int) *Downloader {
	return &Downloader{
		d:       downloader.NewDownloader().SetSavePath(savePath).SetBreakPoint(true).SetTimeOut(timeout),
		retries: retries,
	}
}

// DownloadFile 下载单个文件（如封面）到 savePath，不重试
func (dl *Downloader) DownloadFile(url, fileName string) error {
	task, err := dl.d.NewDownloadTask(url)
	if err != nil {
		return err
	}
	return task.SetFileName(fileName).Download()
}

// 首次不使用反代，失败后按配置切换并复用任务续传
func (dl *Downloader) DownloadMusic(ctx context.Context, req Request, progress func(Progress)) error {
	task, err := dl.d.NewDownloadTask(req.URL)
	if err != nil {
		return err
	}
	if len(req.Policy.HostReplace) != 0 {
		host := task.GetHostName()
		for old, replacement := range req.Policy.HostReplace {
			host = strings.ReplaceAll(host, old, replacement)
		}
		task.ReplaceHostName(host)
	}
	if req.Policy.ForceHTTPS {
		task.ForceHttps()
	}
	if req.Policy.ForceMultiThread {
		task.ForceMultiThread()
	}
	for key, value := range req.Headers {
		task.AddHeader(key, value)
	}
	task.SetFileName(req.FileName)

	attempts := dl.retries + 1
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		lastErr = dl.watch(ctx, task, attempt, progress)
		if lastErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			task.CleanTempFiles()
			return ctx.Err()
		}
	}
	task.CleanTempFiles()
	return lastErr
}

func (dl *Downloader) watch(ctx context.Context, task *downloader.DownloadTask, attempt int, progress func(Progress)) error {
	errCh := task.DownloadWithChannel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-errCh:
			return err
		case <-ctx.Done():
			task.Cancel()
			<-errCh
			return ctx.Err()
		case <-ticker.C:
			written, total := task.GetWrittenBytes(), task.GetFileSize()
			if total != 0 && written != 0 {
				progress(Progress{
					Attempt: attempt,
					Written: written,
					Total:   total,
					Speed:   task.CalculateSpeed(time.Millisecond * 500),
				})
			}
		}
	}
}
