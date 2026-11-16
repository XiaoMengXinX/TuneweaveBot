package store

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestRequestsAndStats(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "new.db"))

	a := &SongInfo{Provider: "netease", TrackID: "1", SongName: "晴天", SongArtists: "周杰伦", Duration: 200, MusicSize: 1000, EmbPicSize: 10, FileID: "a"}
	b := &SongInfo{Provider: "qq", TrackID: "2", SongName: "100%_love", Duration: 100, MusicSize: 500, FileID: "b"}
	for _, song := range []*SongInfo{a, b} {
		if err := s.Save(song); err != nil {
			t.Fatal(err)
		}
		if song.ID == 0 {
			t.Fatal("Save 后应带回行 ID")
		}
	}
	// 重新缓存同一首歌时行 ID 不变，点歌记录才能继续指向它
	again := &SongInfo{Provider: "netease", TrackID: "1", SongName: "晴天", SongArtists: "周杰伦", Duration: 200, MusicSize: 1000, EmbPicSize: 10, FileID: "a2"}
	if err := s.Save(again); err != nil {
		t.Fatal(err)
	}
	if again.ID != a.ID {
		t.Fatalf("upsert 后 ID = %d, want %d", again.ID, a.ID)
	}

	for _, r := range []struct {
		user, chat int64
		song       uint
		source     string
	}{{7, 7, a.ID, "command"}, {7, -100, a.ID, "link"}, {7, 7, b.ID, "webapp"}, {8, -100, a.ID, "guest"}} {
		if err := s.RecordRequest(r.user, r.chat, r.song, r.source); err != nil {
			t.Fatal(err)
		}
	}

	summary, err := s.UserSummary(7)
	if err != nil || summary != (UserSummary{Songs: 2, Duration: 300, Size: 1510}) {
		t.Fatalf("UserSummary = %+v, %v", summary, err)
	}
	// 导入的旧数据里封面大小可能为 NULL，不能让整行的大小丢失
	if err := s.db.Exec("UPDATE song_infos SET emb_pic_size = NULL WHERE id = ?", b.ID).Error; err != nil {
		t.Fatal(err)
	}
	if songs, size, err := s.CacheTotals(); err != nil || songs != 2 || size != 1510 {
		t.Fatalf("CacheTotals = %d 首 / %d 字节, %v", songs, size, err)
	}
	if summary, _ := s.UserSummary(7); summary.Size != 1510 {
		t.Fatalf("含 NULL 列时 UserSummary.Size = %d, want 1510", summary.Size)
	}

	songs, total, err := s.UserSongs(7, "", OrderRecent, 10, 0)
	if err != nil || total != 2 || len(songs) != 2 {
		t.Fatalf("UserSongs = %d 首 / total %d, %v", len(songs), total, err)
	}
	for _, song := range songs {
		if song.ID == a.ID && (song.Times != 2 || song.LastAt == 0) {
			t.Errorf("点歌次数与时间错误: %+v", song)
		}
	}
	// 按歌名排序：英文不区分大小写，中文在英文之后
	names := func(order SongOrder) []string {
		songs, _, _ := s.UserSongs(7, "", order, 10, 0)
		var out []string
		for _, song := range songs {
			out = append(out, song.SongName)
		}
		return out
	}
	if got := names(OrderTitleAsc); !slices.Equal(got, []string{"100%_love", "晴天"}) {
		t.Errorf("A-Z = %v", got)
	}
	if got := names(OrderTitleDesc); !slices.Equal(got, []string{"晴天", "100%_love"}) {
		t.Errorf("Z-A = %v", got)
	}
	if got, want := names("unknown"), names(OrderRecent); !slices.Equal(got, want) {
		t.Errorf("未知排序应按最近点歌, got %v want %v", got, want)
	}
	// % 与 _ 按字面匹配
	if songs, total, _ := s.UserSongs(7, "100%_", OrderRecent, 10, 0); total != 1 || songs[0].ID != b.ID {
		t.Errorf("搜索结果错误: total %d", total)
	}
	if _, total, _ := s.ListSongs("周杰伦", "netease", 10, 0); total != 1 {
		t.Errorf("ListSongs total = %d, want 1", total)
	}

	stats, err := s.Stats(time.Now(), 7, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Cached) != 7 || stats.Cached[6].Count != 2 || stats.Requests[6].Count != 4 {
		t.Errorf("今日新增缓存/点歌量 = %+v / %+v", stats.Cached, stats.Requests)
	}
	if len(stats.TopSongs) != 2 || stats.TopSongs[0].ID != a.ID || stats.TopSongs[0].Count != 3 {
		t.Errorf("TopSongs = %+v", stats.TopSongs)
	}
	if len(stats.TopUsers) != 2 || stats.TopUsers[0] != (IDCount{ID: 7, Count: 3}) {
		t.Errorf("TopUsers = %+v", stats.TopUsers)
	}
	if len(stats.RequestsBySource) != 4 || len(stats.CacheByProvider) != 2 {
		t.Errorf("分布 = %+v / %+v", stats.RequestsBySource, stats.CacheByProvider)
	}

	filtered, err := s.Stats(time.Now(), 7, "qq")
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Cached[6].Count != 1 || filtered.Requests[6].Count != 1 || len(filtered.TopSongs) != 1 {
		t.Errorf("按 provider 过滤 = %+v / %+v", filtered.Cached, filtered.Requests)
	}

	// 删除缓存后，该歌曲不再出现在用户列表中
	if _, _, err := s.DeleteByRef("qq", "2"); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := s.UserSongs(7, "", OrderRecent, 10, 0); total != 1 {
		t.Errorf("删除后 total = %d, want 1", total)
	}
}
