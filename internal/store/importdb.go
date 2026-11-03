package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gorm.io/gorm"
)

// ImportResult 汇总一次导入。
type ImportResult struct {
	Format   string // 源库格式
	Eligible int64  // 源库中可导入的记录
	Imported int64  // 实际写入的记录，其余为本库已有而跳过
}

// importColumns 是 song_infos 中可直接对应的列。
var importColumns = []string{
	"song_name", "song_artists", "song_album", "file_ext", "music_size", "pic_size", "emb_pic_size",
	"bit_rate", "duration", "file_id", "thumb_file_id", "from_user_id", "from_user_name", "from_chat_id", "from_chat_name",
}

// Import 把另一个数据库的歌曲缓存导入本库，只读取源库。支持 TuneweaveBot 的数据库，
// 以及 Music163bot（Go / v2 / Next）以 music_id 为键的数据库（按网易云歌曲导入）。
// 已删除或没有 file_id 的记录跳过，本库已有的歌曲不覆盖。
func (s *Store) Import(path string) (ImportResult, error) {
	var res ImportResult
	src, err := filepath.Abs(path)
	if err != nil {
		return res, err
	}
	if _, err := os.Stat(src); err != nil {
		return res, fmt.Errorf("无法读取源数据库: %w", err)
	}
	// ATTACH 只对当前连接生效，整个导入固定在同一连接上
	err = s.db.Connection(func(tx *gorm.DB) error {
		if err := tx.Exec("ATTACH DATABASE ? AS src", src).Error; err != nil {
			return fmt.Errorf("打开源数据库失败: %w", err)
		}
		defer tx.Exec("DETACH DATABASE src")

		var cols []struct{ Name string }
		if err := tx.Raw("SELECT name FROM pragma_table_info('song_infos', 'src')").Scan(&cols).Error; err != nil {
			return err
		}
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = c.Name
		}

		var target, source string
		switch {
		case slices.Contains(names, "provider") && slices.Contains(names, "track_id"):
			res.Format = "TuneweaveBot"
			target = "provider, track_id, provider_name, page_url, share_text"
			source = "provider, track_id, provider_name, page_url, share_text"
		case slices.Contains(names, "music_id"):
			res.Format = "Music163bot"
			target = "provider, track_id, provider_name, page_url, share_text"
			source = "'netease', CAST(music_id AS TEXT), '网易云音乐', " +
				"'https://music.163.com/song/' || music_id, 'https://music.163.com/song?id=' || music_id"
		case len(names) == 0:
			return errors.New("源数据库中没有 song_infos 表")
		default:
			return errors.New("无法识别源数据库的 song_infos 表结构")
		}
		for _, c := range importColumns {
			if !slices.Contains(names, c) {
				return fmt.Errorf("源数据库的 song_infos 表缺少列 %s", c)
			}
		}

		where := "deleted_at IS NULL AND COALESCE(file_id, '') <> ''"
		if res.Format == "Music163bot" {
			where += " AND music_id > 0"
		} else {
			where += " AND provider <> '' AND track_id <> ''"
		}
		if err := tx.Raw("SELECT COUNT(*) FROM src.song_infos WHERE " + where).Scan(&res.Eligible).Error; err != nil {
			return err
		}
		shared := strings.Join(importColumns, ", ")
		insert := tx.Exec(fmt.Sprintf(
			"INSERT INTO main.song_infos (created_at, updated_at, %s, %s) "+
				"SELECT created_at, updated_at, %s, %s FROM src.song_infos WHERE %s "+
				"ON CONFLICT(provider, track_id) DO NOTHING",
			target, shared, source, shared, where))
		if insert.Error != nil {
			return fmt.Errorf("导入失败: %w", insert.Error)
		}
		res.Imported = insert.RowsAffected
		return nil
	})
	return res, err
}
