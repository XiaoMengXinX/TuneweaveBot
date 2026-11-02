package store

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm/logger"
)

func open(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path, logger.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestFreshDatabase(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "new.db"))

	if _, ok := s.GetByRef("netease", "1"); ok {
		t.Error("空库不应命中缓存")
	}
	total, byUser, byChat := s.Counts(1, 2)
	if total != 0 || byUser != 0 || byChat != 0 {
		t.Errorf("空库计数应为 0, got %d/%d/%d", total, byUser, byChat)
	}
}

func TestSaveUpsert(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "new.db"))

	first := &SongInfo{Provider: "netease", TrackID: "300", SongName: "v1", FileID: "id-1"}
	if err := s.Save(first); err != nil {
		t.Fatal(err)
	}
	second := &SongInfo{Provider: "netease", TrackID: "300", SongName: "v2", FileID: "id-2"}
	if err := s.Save(second); err != nil {
		t.Fatal(err)
	}

	total, _, _ := s.Counts(0, 0)
	if total != 1 {
		t.Fatalf("同 (provider, track_id) 应更新而非插入, 总数 = %d", total)
	}
	song, ok := s.GetByRef("netease", "300")
	if !ok || song.SongName != "v2" || song.FileID != "id-2" {
		t.Errorf("更新后的值错误: %+v", song)
	}
}

func TestProviderTrackKeys(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "new.db"))

	if err := s.Save(&SongInfo{Provider: "netease", TrackID: "1", SongName: "网易云"}); err != nil {
		t.Fatal(err)
	}
	// 同一 track_id 在不同 provider 下互不冲突
	if err := s.Save(&SongInfo{Provider: "other", TrackID: "1", SongName: "其他平台"}); err != nil {
		t.Fatal(err)
	}
	// 非数字 track_id 同样支持
	if err := s.Save(&SongInfo{Provider: "other", TrackID: "abc", SongName: "字符串 ID"}); err != nil {
		t.Fatal(err)
	}

	for provider, want := range map[string]string{"netease": "网易云", "other": "其他平台"} {
		if song, ok := s.GetByRef(provider, "1"); !ok || song.SongName != want {
			t.Errorf("GetByRef(%s, 1) = %+v, %v", provider, song, ok)
		}
	}
	if song, ok := s.GetByRef("other", "abc"); !ok || song.SongName != "字符串 ID" {
		t.Errorf("GetByRef(other, abc) = %+v, %v", song, ok)
	}
	if total, _, _ := s.Counts(0, 0); total != 3 {
		t.Errorf("总数 = %d, want 3", total)
	}

	// provider 为空应报错
	if err := s.Save(&SongInfo{TrackID: "2", SongName: "缺 provider"}); err == nil {
		t.Error("provider 为空时应报错")
	}
	// track_id 为空应报错
	if err := s.Save(&SongInfo{Provider: "other"}); err == nil {
		t.Error("track_id 为空时应报错")
	}

	if name, ok, err := s.DeleteByRef("other", "abc"); err != nil || !ok || name != "字符串 ID" {
		t.Errorf("DeleteByRef = %q, %v, %v", name, ok, err)
	}
}

func TestSaveAfterDelete(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "new.db"))

	if err := s.Save(&SongInfo{Provider: "netease", TrackID: "400", SongName: "歌", FileID: "id"}); err != nil {
		t.Fatal(err)
	}
	name, ok, err := s.DeleteByRef("netease", "400")
	if err != nil || !ok || name != "歌" {
		t.Fatalf("删除失败: %v %v %q", err, ok, name)
	}
	if _, ok := s.GetByRef("netease", "400"); ok {
		t.Error("删除后不应命中")
	}
	if _, ok, err := s.DeleteByRef("netease", "400"); err != nil || ok {
		t.Errorf("重复删除应返回不存在: %v %v", ok, err)
	}

	// 删除是软删除，重新 Save 应清除软删除标记并恢复记录
	if err := s.Save(&SongInfo{Provider: "netease", TrackID: "400", SongName: "歌", FileID: "id2"}); err != nil {
		t.Fatal(err)
	}
	if song, ok := s.GetByRef("netease", "400"); !ok || song.FileID != "id2" {
		t.Errorf("重新缓存应恢复记录: %+v %v", song, ok)
	}
}
