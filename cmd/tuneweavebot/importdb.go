package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	gormlogger "gorm.io/gorm/logger"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/config"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

// importDB 把 from 的歌曲缓存导入配置中 [database] path 指定的数据库。
func importDB(cfgPath, from string) error {
	if from == "" {
		return errors.New("请用 -from 指定要导入的数据库")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	if sameFile(from, cfg.Database.Path) {
		return fmt.Errorf("源数据库与 [database] path（%s）是同一个文件", cfg.Database.Path)
	}
	st, err := store.Open(cfg.Database.Path, gormlogger.Default.LogMode(gormlogger.Silent))
	if err != nil {
		return err
	}
	defer st.Close()
	res, err := st.Import(from)
	if err != nil {
		return err
	}
	fmt.Printf("已从 %s（%s）导入 %d 首歌曲到 %s", from, res.Format, res.Imported, cfg.Database.Path)
	if skipped := res.Eligible - res.Imported; skipped > 0 {
		fmt.Printf("，%d 首已存在而跳过", skipped)
	}
	fmt.Println()
	return nil
}

func sameFile(a, b string) bool {
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	if err1 == nil && err2 == nil {
		return os.SameFile(ai, bi)
	}
	aa, _ := filepath.Abs(a)
	ba, _ := filepath.Abs(b)
	return aa == ba
}
