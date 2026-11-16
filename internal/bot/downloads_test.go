package bot

import (
	"context"
	"testing"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

func TestDownloadTracker(t *testing.T) {
	a := &App{musicSem: semaphore.NewWeighted(1)}
	ref := provider.Ref{Provider: "netease", ID: "1"}
	group := requestOrigin{userID: 7, chatID: -100, chatName: "音乐群", source: sourceLink}

	first, release, err := a.acquireDownload(context.Background(), group, ref)
	if err != nil {
		t.Fatal(err)
	}
	// 下载位已满，第二个任务处于排队状态
	queued := make(chan func())
	go func() {
		_, release2, err := a.acquireDownload(context.Background(), requestOrigin{userID: 8, chatID: 8}, ref)
		if err != nil {
			t.Error(err)
		}
		queued <- release2
	}()
	deadline := time.Now().Add(time.Second)
	for len(a.downloads.snapshot()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	tasks := a.downloads.snapshot()
	if len(tasks) != 2 || tasks[0].Phase != phaseResolving || tasks[0].StartedAt == 0 || tasks[1].Phase != phaseWaiting || tasks[1].StartedAt != 0 {
		t.Fatalf("排队状态错误: %+v", tasks)
	}
	if tasks[0].ChatName != "音乐群" || tasks[0].ChatID != -100 || tasks[0].Source != sourceLink {
		t.Errorf("任务应带上发起的聊天: %+v", tasks[0])
	}

	first.setTitle("插件上报的歌名", "专辑")
	first.setSong(store.SongInfo{SongName: "晴天", SongArtists: "周杰伦", SongAlbum: "叶惠美"})
	first.setTitle("不应覆盖", "")
	first.progress(phaseDownloading, 100, 1000)
	if got := a.downloads.snapshot()[0]; got.Title != "晴天" || got.Artists != "周杰伦" || got.Phase != phaseDownloading || got.Written != 100 || got.Total != 1000 {
		t.Errorf("进度错误: %+v", got)
	}
	first.setPhase(phaseUploading)
	if got := a.downloads.snapshot()[0]; got.Phase != phaseUploading || got.Written != 0 || got.Total != 0 {
		t.Errorf("切换阶段后应清空进度: %+v", got)
	}

	release()
	release2 := <-queued
	if tasks := a.downloads.snapshot(); len(tasks) != 1 || tasks[0].Phase != phaseResolving {
		t.Errorf("释放后排队的任务应开始: %+v", tasks)
	}
	release2()
	if tasks := a.downloads.snapshot(); len(tasks) != 0 {
		t.Errorf("全部释放后不应有任务: %+v", tasks)
	}

	// 取消排队中的任务也要从列表移除
	_, hold, _ := a.acquireDownload(context.Background(), group, ref)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := a.acquireDownload(ctx, group, ref); err == nil {
		t.Fatal("ctx 取消后应返回错误")
	}
	if tasks := a.downloads.snapshot(); len(tasks) != 1 {
		t.Errorf("取消的任务应移除: %+v", tasks)
	}
	hold()
}

func TestDownloadSpeed(t *testing.T) {
	task := &downloadTask{}
	task.progress(phaseDownloading, 0, 1000)
	task.sampleAt = task.sampleAt.Add(-2 * time.Second) // 模拟 2 秒前的采样
	task.progress(phaseDownloading, 400, 1000)
	if s := task.snapshot(); s.Speed < 190 || s.Speed > 210 {
		t.Errorf("速度应约为 200 B/s, got %v", s.Speed)
	}
}
