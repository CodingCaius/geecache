package geecache

import (
	"github.com/sirupsen/logrus"
)

// debug < info < warn < error < fatal

// Logger 是一个最小的接口，允许我们使用结构化记录器，包括（但不限于）logrus。
type Logger interface {
	Error() Logger

	Warn() Logger

	Info() Logger

	Debug() Logger

	//ErrorField 是带有错误值的字段,用于在日志条目中添加一个带有错误值的字段
	ErrorField(label string, err error) Logger

	//StringField 用于在日志条目中添加一个带有字符串值的字段。
	StringField(label string, val string) Logger

	// WithFields 用于添加多个键值对（字段）到日志条目中。
	WithFields(fields map[string]interface{}) Logger

	//最后调用 Printf 以在给定的位置发出日志等级,以格式化消息发出日志条目
	Printf(format string, args ...interface{})
}

// LogrusLogger 是 Logger 的一个实现
type LogrusLogger struct {
	// 表示Logrus库中的日志实体
	Entry *logrus.Entry
	// 表示日志级别
	level logrus.Level
}

func (l LogrusLogger) Info() Logger {
	return LogrusLogger{
		Entry: l.Entry,
		level: logrus.InfoLevel,
	}
}

func (l LogrusLogger) Warn() Logger {
		return LogrusLogger{
		Entry: l.Entry,
		level: logrus.WarnLevel,
	}
}

func (l LogrusLogger) Debug() Logger {
	return LogrusLogger{
		Entry: l.Entry,
		level: logrus.DebugLevel,
	}
}

func (l LogrusLogger) Error() Logger {
	return LogrusLogger{
		Entry: l.Entry,
		level: logrus.ErrorLevel,
	}
}

func (l LogrusLogger) WithFields(fields map[string]interface{}) Logger {
	return LogrusLogger{
		// 使用 Logrus 库的 WithFields 方法，该方法返回一个新的 Logrus 日志实体，包含了原始实体的字段以及传递进来的额外字段。
		Entry: l.Entry.WithFields(fields),
		level: l.level,
	}
}