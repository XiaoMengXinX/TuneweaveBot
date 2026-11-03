package store

import (
	"fmt"
	"slices"
	"strings"

	"gorm.io/gorm"
)

// SchemaError 表示数据库结构不符合 TuneweaveBot 的要求。
type SchemaError struct {
	Path   string
	Detail string
	Legacy bool // Music163bot 的 song_infos（以 music_id 为键）
}

func (e *SchemaError) Error() string {
	msg := fmt.Sprintf("数据库 %s 的结构不符合 TuneweaveBot 要求：%s", e.Path, e.Detail)
	if e.Legacy {
		msg += "。这是 Music163bot 的数据库，请把 [database] path 指向新的数据库文件，再执行 " +
			"TuneweaveBot import-db -from " + e.Path + " 导入"
	}
	return msg
}

// requiredTables 是每张表必需的列，取自对应的 gorm 模型，与模型定义保持同步。
func requiredTables(db *gorm.DB) (map[string][]string, error) {
	tables := map[string][]string{}
	for _, model := range []any{&SongInfo{}, &PluginKV{}} {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return nil, err
		}
		tables[stmt.Schema.Table] = stmt.Schema.DBNames
	}
	return tables, nil
}

func tableColumns(db *gorm.DB, table string) ([]string, error) {
	var cols []struct{ Name string }
	if err := db.Raw("SELECT name FROM pragma_table_info(?)", table).Scan(&cols).Error; err != nil {
		return nil, err
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names, nil
}

// validateSchema 检查已存在的表是否具备模型要求的列；requireAll 时还要求表齐全，
// 且存在缓存依赖的 (provider, track_id) 唯一索引。
func validateSchema(db *gorm.DB, path string, requireAll bool) error {
	tables, err := requiredTables(db)
	if err != nil {
		return err
	}
	for _, table := range []string{"song_infos", "plugin_kv"} {
		cols, err := tableColumns(db, table)
		if err != nil {
			return err
		}
		if len(cols) == 0 {
			if requireAll {
				return &SchemaError{Path: path, Detail: "缺少表 " + table}
			}
			continue
		}
		var missing []string
		for _, want := range tables[table] {
			if !slices.Contains(cols, want) {
				missing = append(missing, want)
			}
		}
		if len(missing) > 0 {
			return &SchemaError{
				Path:   path,
				Detail: fmt.Sprintf("表 %s 缺少列 %s", table, strings.Join(missing, ", ")),
				Legacy: table == "song_infos" && slices.Contains(cols, "music_id"),
			}
		}
	}
	if requireAll {
		ok, err := hasUniqueIndex(db, "song_infos", "provider", "track_id")
		if err != nil {
			return err
		}
		if !ok {
			return &SchemaError{Path: path, Detail: "表 song_infos 缺少 (provider, track_id) 唯一索引"}
		}
	}
	return nil
}

func hasUniqueIndex(db *gorm.DB, table string, cols ...string) (bool, error) {
	var indexes []struct {
		Name   string
		Unique bool
	}
	if err := db.Raw(`SELECT name, "unique" FROM pragma_index_list(?)`, table).Scan(&indexes).Error; err != nil {
		return false, err
	}
	for _, idx := range indexes {
		if !idx.Unique {
			continue
		}
		var info []struct{ Name string }
		if err := db.Raw("SELECT name FROM pragma_index_info(?) ORDER BY seqno", idx.Name).Scan(&info).Error; err != nil {
			return false, err
		}
		got := make([]string, len(info))
		for i, c := range info {
			got[i] = c.Name
		}
		if slices.Equal(got, cols) {
			return true, nil
		}
	}
	return false, nil
}
