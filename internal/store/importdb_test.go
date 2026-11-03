package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// legacySchema 是 Music163bot（Go / v2 / Next）的 song_infos 表。
const legacySchema = `CREATE TABLE song_infos (
	id integer PRIMARY KEY AUTOINCREMENT, created_at datetime, updated_at datetime, deleted_at datetime,
	music_id integer, song_name text, song_artists text, song_album text, file_ext text,
	music_size integer, pic_size integer, emb_pic_size integer, bit_rate integer, duration integer,
	file_id text, thumb_file_id text, from_user_id integer, from_user_name text, from_chat_id integer, from_chat_name text)`

func rawDB(t *testing.T, path string, stmts ...string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func openStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path, logger.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenRejectsLegacyWithoutTouchingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db := rawDB(t, path, legacySchema)

	_, err := Open(path, logger.Discard)
	var se *SchemaError
	if !errors.As(err, &se) || !se.Legacy || !strings.Contains(err.Error(), "import-db -from") {
		t.Fatalf("err = %v，应提示用 import-db 导入", err)
	}
	var n int64
	db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE name = 'goose_db_version' OR name = 'plugin_kv'").Scan(&n)
	if n != 0 {
		t.Error("结构不符时不应在旧库上执行迁移")
	}
}

func TestOpenRejectsIncompleteSchema(t *testing.T) {
	for _, c := range []struct {
		name, stmt, want string
	}{
		{"missing column", "ALTER TABLE song_infos DROP COLUMN share_text", "缺少列 share_text"},
		{"missing unique index", "DROP INDEX idx_song_infos_provider_track", "唯一索引"},
		{"missing table", "DROP TABLE plugin_kv", "缺少表 plugin_kv"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cache.db")
			s, err := Open(path, logger.Discard)
			if err != nil {
				t.Fatal(err)
			}
			_ = s.Close()
			rawDB(t, path, c.stmt)
			if _, err := Open(path, logger.Discard); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestImportMusic163bot(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "next.db")
	rawDB(t, src, legacySchema,
		`INSERT INTO song_infos (created_at, music_id, song_name, file_ext, bit_rate, duration, file_id, from_user_id) VALUES
			('2025-01-01 00:00:00', 186016, '晴天', 'flac', 900000, 269, 'FILE1', 7),
			('2025-01-02 00:00:00', 5257138, '七里香', 'mp3', 320000, 299, 'FILE2', 7)`,
		`INSERT INTO song_infos (music_id, song_name, file_id, deleted_at) VALUES (1, '已删除', 'X', '2025-01-03')`,
		`INSERT INTO song_infos (music_id, song_name, file_id) VALUES (2, '无文件', '')`)

	s := openStore(t, filepath.Join(dir, "cache.db"))
	if err := s.Save(&SongInfo{Provider: "netease", TrackID: "5257138", SongName: "本库已有", FileID: "MINE"}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Import(src)
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != "Music163bot" || res.Eligible != 2 || res.Imported != 1 {
		t.Fatalf("res = %+v", res)
	}
	got, ok := s.GetByRef("netease", "186016")
	if !ok || got.SongName != "晴天" || got.FileID != "FILE1" || got.ProviderName != "网易云音乐" ||
		got.PageURL != "https://music.163.com/song/186016" || got.ShareText != "https://music.163.com/song?id=186016" ||
		got.BitRate != 900000 || got.Duration != 269 || got.FromUserID != 7 {
		t.Fatalf("导入结果 = %+v", got)
	}
	if mine, _ := s.GetByRef("netease", "5257138"); mine.FileID != "MINE" {
		t.Error("本库已有的歌曲不应被覆盖")
	}
	if again, err := s.Import(src); err != nil || again.Imported != 0 {
		t.Errorf("重复导入 = %+v, %v", again, err)
	}
}

func TestImportTuneweaveBot(t *testing.T) {
	dir := t.TempDir()
	src := openStore(t, filepath.Join(dir, "old.db"))
	for _, id := range []string{"a", "b"} {
		if err := src.Save(&SongInfo{Provider: "spotify", TrackID: id, ProviderName: "Spotify", PageURL: "p" + id, ShareText: "s" + id, FileID: "F" + id}); err != nil {
			t.Fatal(err)
		}
	}
	s := openStore(t, filepath.Join(dir, "cache.db"))
	res, err := s.Import(filepath.Join(dir, "old.db"))
	if err != nil || res.Format != "TuneweaveBot" || res.Imported != 2 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if got, ok := s.GetByRef("spotify", "b"); !ok || got.PageURL != "pb" || got.FileID != "Fb" {
		t.Fatalf("got = %+v", got)
	}
}

func TestImportRejectsUnknownSource(t *testing.T) {
	dir := t.TempDir()
	rawDB(t, filepath.Join(dir, "other.db"), "CREATE TABLE notes (id integer)")
	s := openStore(t, filepath.Join(dir, "cache.db"))
	if _, err := s.Import(filepath.Join(dir, "other.db")); err == nil || !strings.Contains(err.Error(), "没有 song_infos") {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.Import(filepath.Join(dir, "missing.db")); err == nil {
		t.Fatal("源库不存在时应报错")
	}
}
