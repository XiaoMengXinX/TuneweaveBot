-- 插件键值存储（Phase 2 使用），按插件命名空间隔离
-- +goose Up
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
