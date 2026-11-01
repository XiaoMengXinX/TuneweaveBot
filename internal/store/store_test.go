package store

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// v2SongInfo 与 v2 的模型完全一致，用于模拟 v2 程序创建的 cache.db
type v2SongInfo struct {
	gorm.Model
	MusicID      int
	SongName     string
	SongArtists  string
	SongAlbum    string
	FileExt      string
	MusicSize    int
	PicSize      int
	EmbPicSize   int
	BitRate      int
	Duration     int
	FileID       string
	ThumbFileID  string
	FromUserID   int64
	FromUserName string
	FromChatID   int64
	FromChatName string
}

func (v2SongInfo) TableName() string { return "song_infos" }

func createV2DB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cache.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("song_infos").AutoMigrate(&v2SongInfo{}); err != nil {
		t.Fatal(err)
	}
	rows := []v2SongInfo{
		{MusicID: 100, SongName: "旧版本歌", FileID: "old-file-id", FromUserID: 1},
		{MusicID: 100, SongName: "新版本歌", FileID: "new-file-id", FromUserID: 2},
		{MusicID: 200, SongName: "另一首", FileID: "f2", FromUserID: 2},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	_ = sqlDB.Close()
	return path
}

func open(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path, logger.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrateFromV2Database(t *testing.T) {
	s := open(t, createV2DB(t))

	song, ok := s.GetByMusicID(100)
	if !ok {
		t.Fatal("迁移后应能查到 music_id=100")
	}
	if song.SongName != "新版本歌" {
		t.Errorf("去重应保留最新一行, got %q", song.SongName)
	}
	if _, ok := s.GetByMusicID(200); !ok {
		t.Error("非重复行应保留")
	}

	total, byUser1, _ := s.Counts(1, 100)
	if total != 2 {
		t.Errorf("总数 = %d, want 2", total)
	}
	if byUser1 != 0 {
		t.Errorf("被去重掉的行不应计入, got %d", byUser1)
	}
}

func TestFreshDatabase(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "new.db"))

	if _, ok := s.GetByMusicID(1); ok {
		t.Error("空库不应命中缓存")
	}
	total, byUser, byChat := s.Counts(1, 2)
	if total != 0 || byUser != 0 || byChat != 0 {
		t.Errorf("空库计数应为 0, got %d/%d/%d", total, byUser, byChat)
	}
}

func TestSaveUpsert(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "new.db"))

	first := &SongInfo{MusicID: 300, SongName: "v1", FileID: "id-1"}
	if err := s.Save(first); err != nil {
		t.Fatal(err)
	}
	second := &SongInfo{MusicID: 300, SongName: "v2", FileID: "id-2"}
	if err := s.Save(second); err != nil {
		t.Fatal(err)
	}

	total, _, _ := s.Counts(0, 0)
	if total != 1 {
		t.Fatalf("同 music_id 应更新而非插入, 总数 = %d", total)
	}
	song, ok := s.GetByMusicID(300)
	if !ok || song.SongName != "v2" || song.FileID != "id-2" {
		t.Errorf("更新后的值错误: %+v", song)
	}
}

func TestProviderTrackKeys(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "new.db"))

	if err := s.Save(&SongInfo{Provider: "netease", TrackID: "1", MusicID: 1, SongName: "网易云"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(&SongInfo{Provider: "other", TrackID: "1", SongName: "其他平台"}); err != nil {
		t.Fatal(err)
	}
	// 非数字 ID 不写 music_id
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

	// 未指定 provider 时默认写入 netease
	if err := s.Save(&SongInfo{MusicID: 2, SongName: "默认"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.GetByRef("netease", "2"); !ok {
		t.Error("默认 provider 应写入 netease")
	}

	if err := s.Save(&SongInfo{Provider: "other"}); err == nil {
		t.Error("track_id 与 music_id 均为空时应报错")
	}

	if name, ok, err := s.DeleteByRef("other", "abc"); err != nil || !ok || name != "字符串 ID" {
		t.Errorf("DeleteByRef = %q, %v, %v", name, ok, err)
	}
}

func TestMigrateNullMusicID(t *testing.T) {
	path := createV2DB(t)
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO song_infos (music_id, song_name, file_id) VALUES (NULL, '无 ID 歌曲', 'legacy-file')").Error; err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.Raw("SELECT id FROM song_infos WHERE music_id IS NULL").Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	_ = sqlDB.Close()

	s := open(t, path)
	song, ok := s.GetByRef("netease", "legacy-"+strconv.FormatInt(id, 10))
	if !ok {
		t.Fatalf("NULL music_id 应回填为 legacy-<id>")
	}
	if song.SongName != "无 ID 歌曲" || song.ProviderName != "网易云音乐" || song.PageURL != "" {
		t.Errorf("迁移后的行 = %+v", song)
	}
	if _, ok := s.GetByRef("netease", "100"); !ok {
		t.Error("旧 music_id 行应回填为 track_id")
	}
}

func TestSaveAfterDelete(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "new.db"))

	if err := s.Save(&SongInfo{MusicID: 400, SongName: "歌", FileID: "id"}); err != nil {
		t.Fatal(err)
	}
	name, ok, err := s.DeleteByMusicID(400)
	if err != nil || !ok || name != "歌" {
		t.Fatalf("删除失败: %v %v %q", err, ok, name)
	}
	if _, ok := s.GetByMusicID(400); ok {
		t.Error("删除后不应命中")
	}
	if _, ok, err := s.DeleteByMusicID(400); err != nil || ok {
		t.Errorf("重复删除应返回不存在: %v %v", ok, err)
	}

	if err := s.Save(&SongInfo{MusicID: 400, SongName: "歌", FileID: "id2"}); err != nil {
		t.Fatal(err)
	}
	if song, ok := s.GetByMusicID(400); !ok || song.FileID != "id2" {
		t.Errorf("重新缓存应恢复记录: %+v %v", song, ok)
	}
}
