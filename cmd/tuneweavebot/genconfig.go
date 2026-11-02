package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	gormlogger "gorm.io/gorm/logger"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/config"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/plugin"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

// genConfig 加载插件、收集各自的配置 schema，把带注释的配置模板打印到 stdout。
func genConfig(ctx context.Context, pluginDir string) error {
	if pluginDir == "" {
		pluginDir = "./plugins"
	}

	st, err := store.Open(":memory:", gormlogger.Default.LogMode(gormlogger.Silent))
	if err != nil {
		return fmt.Errorf("打开内存数据库失败: %w", err)
	}
	defer st.Close()

	log := logrus.New()
	log.SetOutput(os.Stderr)
	log.SetLevel(logrus.WarnLevel)

	manager, err := plugin.Load(ctx, plugin.Config{
		Dir:     pluginDir,
		Runtime: plugin.RuntimeConfig{CallTimeout: 30 * time.Second},
	}, st, log, nil)
	if err != nil {
		return fmt.Errorf("加载插件失败: %w", err)
	}
	defer manager.Close(ctx)

	os.Stdout.Write(config.GenerateTemplate(manager.ConfigSchemas()))
	return nil
}
