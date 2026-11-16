package bot

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

// 下载任务的阶段
const (
	phaseWaiting     = "waiting"     // 等待下载位
	phaseResolving   = "resolving"   // 获取歌曲信息与下载地址（插件可能在此阶段下载）
	phaseDownloading = "downloading" // 宿主下载音频
	phaseProcessing  = "processing"  // 写入标签与封面
	phaseUploading   = "uploading"   // 上传到 Telegram
)

// downloadTracker 记录排队与进行中的下载任务，供面板展示。零值可用。
type downloadTracker struct {
	mu     sync.Mutex
	nextID uint64
	tasks  map[uint64]*downloadTask
}

// downloadTask 是一次下载任务的实时状态，字段由 mu 保护。
type downloadTask struct {
	id       uint64
	origin   requestOrigin
	ref      provider.Ref
	queuedAt time.Time

	mu            sync.Mutex
	startedAt     time.Time // 拿到下载位的时间
	phase         string
	title         string
	artists       string
	album         string
	written       int64
	total         int64
	speed         float64 // 字节/秒
	sampleAt      time.Time
	sampleWritten int64
}

func (t *downloadTracker) add(origin requestOrigin, ref provider.Ref) *downloadTask {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tasks == nil {
		t.tasks = make(map[uint64]*downloadTask)
	}
	t.nextID++
	task := &downloadTask{id: t.nextID, origin: origin, ref: ref, queuedAt: time.Now(), phase: phaseWaiting}
	t.tasks[task.id] = task
	return task
}

func (t *downloadTracker) remove(task *downloadTask) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.tasks, task.id)
}

// downloadSnapshot 是某一时刻的任务状态。
type downloadSnapshot struct {
	ID        uint64  `json:"id"`
	Provider  string  `json:"provider"`
	TrackID   string  `json:"track_id"`
	Title     string  `json:"title"`
	Artists   string  `json:"artists"`
	Album     string  `json:"album"`
	Phase     string  `json:"phase"`
	Written   int64   `json:"written"`
	Total     int64   `json:"total"` // 未知时为 0
	Speed     float64 `json:"speed"`
	UserID    int64   `json:"user_id"`
	UserName  string  `json:"user_name"`
	ChatID    int64   `json:"chat_id"`
	ChatName  string  `json:"chat_name"`
	Source    string  `json:"source"`
	QueuedAt  int64   `json:"queued_at"`
	StartedAt int64   `json:"started_at"` // 尚未拿到下载位时为 0
}

// snapshot 按排队先后返回全部任务。
func (t *downloadTracker) snapshot() []downloadSnapshot {
	t.mu.Lock()
	tasks := make([]*downloadTask, 0, len(t.tasks))
	for _, task := range t.tasks {
		tasks = append(tasks, task)
	}
	t.mu.Unlock()
	slices.SortFunc(tasks, func(a, b *downloadTask) int { return cmp.Compare(a.id, b.id) })

	out := make([]downloadSnapshot, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.snapshot())
	}
	return out
}

func (task *downloadTask) snapshot() downloadSnapshot {
	task.mu.Lock()
	defer task.mu.Unlock()
	s := downloadSnapshot{
		ID: task.id, Provider: task.ref.Provider, TrackID: task.ref.ID,
		Title: task.title, Artists: task.artists, Album: task.album,
		Phase: task.phase, Written: task.written, Total: task.total, Speed: task.speed,
		UserID: task.origin.userID, UserName: task.origin.userName,
		ChatID: task.origin.chatID, ChatName: task.origin.chatName, Source: task.origin.source,
		QueuedAt: task.queuedAt.Unix(),
	}
	if !task.startedAt.IsZero() {
		s.StartedAt = task.startedAt.Unix()
	}
	return s
}

func (task *downloadTask) setPhase(phase string) {
	task.mu.Lock()
	defer task.mu.Unlock()
	task.phase = phase
	task.written, task.total, task.speed = 0, 0, 0
	task.sampleAt, task.sampleWritten = time.Time{}, 0
}

// setSong 记录解析出的歌曲信息。
func (task *downloadTask) setSong(info store.SongInfo) {
	task.mu.Lock()
	defer task.mu.Unlock()
	task.title, task.artists, task.album = info.SongName, info.SongArtists, info.SongAlbum
}

// setTitle 记录插件下载时上报的歌名，已知完整信息时不覆盖。
func (task *downloadTask) setTitle(title, album string) {
	task.mu.Lock()
	defer task.mu.Unlock()
	if task.title == "" && title != "" {
		task.title, task.album = title, album
	}
}

// speedSampleInterval 是计算传输速度的采样间隔。
const speedSampleInterval = time.Second

// progress 更新当前阶段的传输进度，阶段变化时重新计速。
func (task *downloadTask) progress(phase string, written, total int64) {
	task.mu.Lock()
	defer task.mu.Unlock()
	now := time.Now()
	if task.phase != phase || task.sampleAt.IsZero() || written < task.sampleWritten {
		task.phase, task.speed = phase, 0
		task.sampleAt, task.sampleWritten = now, written
	}
	if elapsed := now.Sub(task.sampleAt); elapsed >= speedSampleInterval {
		task.speed = float64(written-task.sampleWritten) / elapsed.Seconds()
		task.sampleAt, task.sampleWritten = now, written
	}
	task.written, task.total = written, total
}

// acquireDownload 登记下载任务并占用一个下载位，release 释放下载位并移除任务。
func (a *App) acquireDownload(ctx context.Context, origin requestOrigin, ref provider.Ref) (task *downloadTask, release func(), err error) {
	task = a.downloads.add(origin, ref)
	if err := a.musicSem.Acquire(ctx, 1); err != nil {
		a.downloads.remove(task)
		return nil, nil, err
	}
	task.mu.Lock()
	task.startedAt, task.phase = time.Now(), phaseResolving
	task.mu.Unlock()
	return task, func() {
		a.downloads.remove(task)
		a.musicSem.Release(1)
	}, nil
}
