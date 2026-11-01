-- 补齐 gorm.Model 软删除索引：每个查询都带 deleted_at IS NULL 条件
-- +goose Up
CREATE INDEX IF NOT EXISTS `idx_song_infos_deleted_at` ON `song_infos`(`deleted_at`);

-- +goose Down
DROP INDEX IF EXISTS `idx_song_infos_deleted_at`;
