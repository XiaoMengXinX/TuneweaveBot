package bot

import (
	"bufio"
	"os"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/config"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/plugin"
)

// completeMissingConfig 检查插件声明但配置文件缺失的字段并补全：
// stdin 是终端时提示用户确认，非终端时自动补全。
func completeMissingConfig(cfg *config.Config, cfgPath string, schemas []plugin.PluginConfigSchema, log *logrus.Logger) {
	missing := config.MissingFields(cfg, schemas)
	if len(missing) == 0 {
		return
	}

	var fields []string
	for _, s := range missing {
		for _, f := range s.Fields {
			fields = append(fields, "[plugins."+s.Name+"] "+f.Key)
		}
	}
	log.Infof("检测到配置缺失 %d 项: %s", len(fields), strings.Join(fields, ", "))

	if stdinIsTerminal() {
		if !promptYes("是否将缺失项写入 " + cfgPath + "?（会保留原有内容与注释）") {
			log.Infoln("已跳过配置补全")
			return
		}
	}
	if err := config.CompleteConfig(cfgPath, missing); err != nil {
		log.Errorf("补全配置失败: %v", err)
		return
	}
	log.Infof("已补全配置到 %s", cfgPath)
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func promptYes(question string) bool {
	os.Stdout.WriteString(question + " [y/N] ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
