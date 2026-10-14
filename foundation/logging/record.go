package logging

import "time"

// Record 是一条结构化日志。
//
// 与 Dart 版的差别只在错误通道：那边是任意 `Object? error`，Go 侧用 `error`——
// 需要传非 error 的描述时写进 Message。
type Record struct {
	Level   Level
	Name    string
	Message any
	Time    time.Time
	Err     error
	Stack   []byte
}

// Exporter 是日志导出器（sink）。
type Exporter interface {
	Export(Record)
}

// EntryOption 给一条日志附加错误或堆栈。
type EntryOption func(*entry)

type entry struct {
	err   error
	stack []byte
}

// WithError 附加错误。
func WithError(err error) EntryOption { return func(e *entry) { e.err = err } }

// WithStack 附加堆栈。
func WithStack(stack []byte) EntryOption { return func(e *entry) { e.stack = stack } }
