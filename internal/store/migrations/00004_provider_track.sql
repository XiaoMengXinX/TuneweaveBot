-- 缓存键从 music_id 迁移到 (provider, track_id)，保留旧数据
-- +goose Up
ALTER TABLE `song_infos` ADD COLUMN `provider` TEXT NOT NULL DEFAULT 'netease';
ALTER TABLE `song_infos` ADD COLUMN `track_id` TEXT NOT NULL DEFAULT '';
ALTER TABLE `song_infos` ADD COLUMN `provider_name` TEXT NOT NULL DEFAULT '';
ALTER TABLE `song_infos` ADD COLUMN `page_url` TEXT NOT NULL DEFAULT '';
ALTER TABLE `song_infos` ADD COLUMN `share_text` TEXT NOT NULL DEFAULT '';

UPDATE `song_infos` SET `track_id` = CASE
    WHEN `music_id` IS NULL THEN 'legacy-' || `id`
    ELSE CAST(`music_id` AS TEXT) END
  WHERE `track_id` = '';
UPDATE `song_infos` SET `provider_name` = '网易云音乐' WHERE `provider_name` = '';
UPDATE `song_infos` SET `page_url` = 'https://music.163.com/song?id=' || `music_id`
  WHERE `page_url` = '' AND `music_id` IS NOT NULL;
UPDATE `song_infos` SET `share_text` = `page_url` WHERE `share_text` = '';

DROP INDEX IF EXISTS `idx_song_infos_music_id`;
CREATE UNIQUE INDEX IF NOT EXISTS `idx_song_infos_provider_track`
  ON `song_infos`(`provider`, `track_id`);

-- +goose Down
-- SQLite 不支持安全删除列，Down 仅恢复旧索引；如需回退请从备份恢复
DROP INDEX IF EXISTS `idx_song_infos_provider_track`;
CREATE UNIQUE INDEX IF NOT EXISTS `idx_song_infos_music_id` ON `song_infos`(`music_id`);
