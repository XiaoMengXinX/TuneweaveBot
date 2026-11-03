package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/bot"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/config"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/logging"
)

var (
	versionName = "v1.0.0"
	commitSHA   = ""
	buildTime   = ""
)

func main() {
	build := bot.BuildInfo{
		RuntimeVer:  runtime.Version(),
		VersionName: versionName,
		CommitSHA:   commitSHA,
		BuildTime:   buildTime,
		BuildArch:   fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}

	cfgPath := flag.String("c", "config.toml", "配置文件路径")
	pluginDir := flag.String("plugins", "./plugins", "gen-config 读取插件 schema 的目录")
	importFrom := flag.String("from", "", "import-db 要导入的源数据库（TuneweaveBot 或 Music163bot）")
	flag.Usage = func() {
		w := flag.CommandLine.Output()
		fmt.Fprintf(w, "Usage:\n")
		fmt.Fprintf(w, "  %s [-c config]			启动 bot\n", os.Args[0])
		fmt.Fprintf(w, "  %s gen-config [-plugins path]	生成带注释的配置模板到 stdout\n", os.Args[0])
		fmt.Fprintf(w, "  %s import-db -from database	从 Music163bot 迁移\n\n", os.Args[0])
		fmt.Fprintf(w, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(w, "\nExample:\n")
		fmt.Fprintf(w, "  %s -c config.toml\n", os.Args[0])
		fmt.Fprintf(w, "  %s gen-config -plugins ./plugins > config.toml\n", os.Args[0])
		fmt.Fprintf(w, "  %s import-db -from ../Music163bot/cache.db\n", os.Args[0])
	}
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd := flag.Arg(0); cmd {
	case "gen-config", "import-db":
		// flag 遇到子命令即停止解析，子命令后的参数需再解析一次
		if err := flag.CommandLine.Parse(flag.Args()[1:]); err != nil {
			os.Exit(2)
		}
		run := func() error { return genConfig(ctx, *pluginDir) }
		if cmd == "import-db" {
			run = func() error { return importDB(*cfgPath, *importFrom) }
		}
		if err := run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	for {
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
			os.Exit(1)
		}
		log := logging.Setup(cfg.Log.Level, cfg.Log.Path)
		log.Printf("TuneweaveBot %s 启动中", build.VersionName)

		// 插件 HTTP 统一走 [plugins] proxy
		pluginHTTP, err := cfg.Plugins.HTTPClient()
		if err != nil {
			log.Errorln(err)
			os.Exit(1)
		}

		reload, err := bot.Run(ctx, cfg, *cfgPath, log, build, pluginHTTP)
		pluginHTTP.CloseIdleConnections()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Errorln(err)
			os.Exit(1)
		}
		if !reload {
			return
		}
		log.Println("/reload：重新加载配置中")
		// 重载须复用信号 ctx，不能调用 stop
	}
}
