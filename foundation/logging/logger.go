package logging

// Logger 是命名 logger 门面。
type Logger struct {
	name    string
	service *Service
}

// Name 返回 logger 名字。
func (l *Logger) Name() string { return l.name }

// Debug 记录 debug 级日志。
func (l *Logger) Debug(message any, opts ...EntryOption) {
	l.service.emitAs(LevelDebug, l.name, message, opts...)
}

// Info 记录 info 级日志。
func (l *Logger) Info(message any, opts ...EntryOption) {
	l.service.emitAs(LevelInfo, l.name, message, opts...)
}

// Warn 记录 warn 级日志。
func (l *Logger) Warn(message any, opts ...EntryOption) {
	l.service.emitAs(LevelWarn, l.name, message, opts...)
}

// Error 记录 error 级日志。
func (l *Logger) Error(message any, opts ...EntryOption) {
	l.service.emitAs(LevelError, l.name, message, opts...)
}
