-- 面板统计用的覆盖索引：统计只读这些窄索引，不再逐行读出带长文本的整行
-- +goose Up
-- 缓存总数、总大小与按 provider 分组
CREATE INDEX IF NOT EXISTS `idx_song_infos_live_provider`
    ON `song_infos`(`deleted_at`, `provider`, `music_size`, `emb_pic_size`);
-- 每日新增缓存
CREATE INDEX IF NOT EXISTS `idx_song_infos_live_created`
    ON `song_infos`(`deleted_at`, `created_at`, `provider`);

-- +goose Down
DROP INDEX IF EXISTS `idx_song_infos_live_created`;
DROP INDEX IF EXISTS `idx_song_infos_live_provider`;
