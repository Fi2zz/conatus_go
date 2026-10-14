// Package logging 是内置日志服务与控制台导出器（对应 Dart 的 logger-console 插件）。
//
// conatus 核心不带 logger，因此本包自带 [Service]（分级、命名 logger、可插拔导出器），
// 并默认挂一个 [ConsoleExporter]。服务可被多 goroutine 并发使用。
package logging

// Level 是日志级别；数值即严重度：debug < info < warn < error。
type Level int

const (
	// LevelDebug 调试级。
	LevelDebug Level = iota
	// LevelInfo 信息级（缺省最小级别）。
	LevelInfo
	// LevelWarn 警告级。
	LevelWarn
	// LevelError 错误级。
	LevelError
)

// ServiceKey 是日志服务在上下文中的服务键。
const ServiceKey = "logger"

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}

// tag 返回控制台行首的单字母标记。
func (l Level) tag() byte {
	switch l {
	case LevelDebug:
		return 'D'
	case LevelInfo:
		return 'I'
	case LevelWarn:
		return 'W'
	case LevelError:
		return 'E'
	default:
		return '?'
	}
}
