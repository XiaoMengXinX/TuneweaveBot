package bot

import (
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
)

// 群内无权限、话题关闭、状态消息已被删除和限流属于预期情况
var expectedErrors = []string{
	"not enough rights to send",
	"TOPIC_CLOSED",
	"message to edit not found",
	"message to delete not found",
	"Too Many Requests",
}

// infoErrors 是正常业务结果（如歌曲无版权），降为 info。
var infoErrors = []string{
	render.NoAudioLog,
}

// expectedErrorHook 将预期的 Telegram 错误降级为 warning、正常业务结果降级为 info，保留原调用位置
type expectedErrorHook struct{}

func (expectedErrorHook) Levels() []logrus.Level {
	return []logrus.Level{logrus.ErrorLevel}
}

func (expectedErrorHook) Fire(entry *logrus.Entry) error {
	for _, s := range infoErrors {
		if strings.HasPrefix(entry.Message, s) {
			entry.Level = logrus.InfoLevel
			return nil
		}
	}
	for _, s := range expectedErrors {
		if strings.Contains(entry.Message, s) {
			entry.Level = logrus.WarnLevel
			return nil
		}
	}
	return nil
}
