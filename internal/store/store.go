// Package store 管理 SQLite 歌曲缓存
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// SongInfo 歌曲缓存，以 (Provider, TrackID) 为缓存键
type SongInfo struct {
	gorm.Model
	Provider     string
	TrackID      string
	ProviderName string
	PageURL      string
	ShareText    string
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

func (SongInfo) TableName() string { return "song_infos" }

type Store struct {
	db *gorm.DB
}

func Open(path string, gormLogger logger.Interface) (*Store, error) {
	db, err := gorm.Open(sqlite.Open(path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"), &gorm.Config{
		Logger:      gormLogger,
		PrepareStmt: true,
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// 先检查已有表，避免在结构不符的数据库（如 Music163bot 的旧库）上执行迁移
	if err := validateSchema(db, path, false); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	// goose 只扫描 FS 根目录
	migrations, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, migrations)
	if err != nil {
		return nil, err
	}
	if _, err := provider.Up(context.Background()); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}
	if err := validateSchema(db, path, true); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// GetByRef 按 (provider, track_id) 查询缓存（自动过滤软删除行）
func (s *Store) GetByRef(provider, trackID string) (SongInfo, bool) {
	var songInfo SongInfo
	err := s.db.Where("provider = ? AND track_id = ?", provider, trackID).First(&songInfo).Error
	if err != nil {
		return songInfo, false
	}
	return songInfo, true
}

// 按 (provider, track_id) 更新缓存，同时清除软删除标记
func (s *Store) Save(songInfo *SongInfo) error {
	if songInfo.Provider == "" {
		return fmt.Errorf("保存缓存失败: provider 不能为空")
	}
	if songInfo.TrackID == "" {
		return fmt.Errorf("保存缓存失败: track_id 不能为空")
	}
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "provider"}, {Name: "track_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"updated_at", "deleted_at", "provider_name", "page_url", "share_text",
			"song_name", "song_artists", "song_album",
			"file_ext", "music_size", "pic_size", "emb_pic_size", "bit_rate", "duration",
			"file_id", "thumb_file_id", "from_user_id", "from_user_name", "from_chat_id", "from_chat_name",
		}),
	}).Create(songInfo).Error
}

func (s *Store) DeleteByRef(provider, trackID string) (name string, ok bool, err error) {
	songInfo, found := s.GetByRef(provider, trackID)
	if !found {
		return "", false, nil
	}
	if err := s.db.Where("provider = ? AND track_id = ?", provider, trackID).Delete(&SongInfo{}).Error; err != nil {
		return "", false, err
	}
	return songInfo.SongName, true, nil
}

// PluginKV 是插件的键值存储行。
type PluginKV struct {
	Plugin    string `gorm:"primaryKey"`
	Key       string `gorm:"primaryKey"`
	Value     []byte
	Secret    bool
	UpdatedAt time.Time
}

func (PluginKV) TableName() string { return "plugin_kv" }

func (s *Store) PluginKVGet(plugin, key string) ([]byte, bool, error) {
	var row PluginKV
	err := s.db.Where("plugin = ? AND key = ?", plugin, key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return row.Value, true, nil
}

func (s *Store) PluginKVSet(plugin, key string, value []byte) error {
	row := PluginKV{Plugin: plugin, Key: key, Value: value, UpdatedAt: time.Now()}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "plugin"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&row).Error
}

func (s *Store) PluginKVDelete(plugin, key string) error {
	return s.db.Where("plugin = ? AND key = ?", plugin, key).Delete(&PluginKV{}).Error
}

func (s *Store) PluginKVList(plugin string) ([]string, error) {
	var keys []string
	err := s.db.Model(&PluginKV{}).Where("plugin = ?", plugin).Order("key").Pluck("key", &keys).Error
	return keys, err
}

func (s *Store) Counts(fromUserID, fromChatID int64) (total, byUser, byChat int64) {
	s.db.Model(&SongInfo{}).Count(&total)
	s.db.Model(&SongInfo{}).Where("from_user_id = ?", fromUserID).Count(&byUser)
	s.db.Model(&SongInfo{}).Where("from_chat_id = ?", fromChatID).Count(&byChat)
	return total, byUser, byChat
}
