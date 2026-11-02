-- 歌曲缓存表，以 (provider, track_id) 为缓存键，适配多 provider 插件模式
-- +goose Up
CREATE TABLE IF NOT EXISTS `song_infos` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` datetime NULL,
    `updated_at` datetime NULL,
    `deleted_at` datetime NULL,
    `provider` text NOT NULL DEFAULT '',
    `track_id` text NOT NULL DEFAULT '',
    `provider_name` text NOT NULL DEFAULT '',
    `page_url` text NOT NULL DEFAULT '',
    `share_text` text NOT NULL DEFAULT '',
    `song_name` text,
    `song_artists` text,
    `song_album` text,
    `file_ext` text,
    `music_size` integer,
    `pic_size` integer,
    `emb_pic_size` integer,
    `bit_rate` integer,
    `duration` integer,
    `file_id` text,
    `thumb_file_id` text,
    `from_user_id` integer,
    `from_user_name` text,
    `from_chat_id` integer,
    `from_chat_name` text
);

CREATE UNIQUE INDEX IF NOT EXISTS `idx_song_infos_provider_track`
    ON `song_infos`(`provider`, `track_id`);
CREATE INDEX IF NOT EXISTS `idx_song_infos_from_user_id` ON `song_infos`(`from_user_id`);
CREATE INDEX IF NOT EXISTS `idx_song_infos_from_chat_id` ON `song_infos`(`from_chat_id`);
CREATE INDEX IF NOT EXISTS `idx_song_infos_created_at` ON `song_infos`(`created_at`);
CREATE INDEX IF NOT EXISTS `idx_song_infos_deleted_at` ON `song_infos`(`deleted_at`);

-- 插件键值存储，按插件命名空间隔离，用于保存 cookie/token 等凭据
CREATE TABLE IF NOT EXISTS `plugin_kv` (
    `plugin` TEXT NOT NULL,
    `key` TEXT NOT NULL,
    `value` BLOB,
    `secret` INTEGER NOT NULL DEFAULT 0,
    `updated_at` datetime,
    PRIMARY KEY (`plugin`, `key`)
);

-- +goose Down
DROP TABLE IF EXISTS `plugin_kv`;
DROP TABLE IF EXISTS `song_infos`;
