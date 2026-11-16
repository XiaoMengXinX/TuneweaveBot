-- 点歌记录，每次点歌一行（含命中缓存），song_id 指向 song_infos.id
-- +goose Up
CREATE TABLE IF NOT EXISTS `song_requests` (
    `id` integer PRIMARY KEY AUTOINCREMENT,
    `created_at` integer NOT NULL,
    `user_id` integer NOT NULL,
    `chat_id` integer NOT NULL,
    `song_id` integer NOT NULL,
    `source` text NOT NULL
);

CREATE INDEX IF NOT EXISTS `idx_song_requests_user_created` ON `song_requests`(`user_id`, `created_at`);
CREATE INDEX IF NOT EXISTS `idx_song_requests_created_at` ON `song_requests`(`created_at`);

-- +goose Down
DROP TABLE IF EXISTS `song_requests`;
