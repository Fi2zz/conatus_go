package logging

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Writer 写入一整行日志。
type Writer func(line string)

// ConsoleOption 定制 [ConsoleExporter]。
type ConsoleOption func(*ConsoleExporter)

// WithWriter 指定写入口（缺省写 stdout）。
func WithWriter(writer Writer) ConsoleOption {
	return func(e *ConsoleExporter) { e.writer = writer }
}

// WithExporterMinLevel 让导出器自身也按级别过滤（缺省不过滤，由服务级别决定）。
func WithExporterMinLevel(level Level) ConsoleOption {
	return func(e *ConsoleExporter) {
		e.minLevel = level
		e.filtered = true
	}
}

// WithShowTime 在行首输出 ISO 时间。
func WithShowTime(show bool) ConsoleOption {
	return func(e *ConsoleExporter) { e.showTime = show }
}

// ConsoleExporter 是控制台导出器：格式 `[I] name  message`。
type ConsoleExporter struct {
	writer   Writer
	minLevel Level
	filtered bool
	showTime bool
}

// NewConsoleExporter 创建控制台导出器。
func NewConsoleExporter(opts ...ConsoleOption) *ConsoleExporter {
	exporter := &ConsoleExporter{writer: stdoutWriter}
	for _, opt := range opts {
		opt(exporter)
	}
	return exporter
}

// Export 导出一条日志；带堆栈时另起一行。
func (e *ConsoleExporter) Export(record Record) {
	if e.filtered && record.Level < e.minLevel {
		return
	}
	e.writer(e.format(record))
	if len(record.Stack) > 0 {
		e.writer(string(record.Stack))
	}
}

func (e *ConsoleExporter) format(record Record) string {
	var builder strings.Builder
	if e.showTime {
		builder.WriteString(record.Time.Format(time.RFC3339Nano))
		builder.WriteByte(' ')
	}
	fmt.Fprintf(&builder, "[%c] %s  %v", record.Level.tag(), record.Name, record.Message)
	if record.Err != nil {
		fmt.Fprintf(&builder, " %v", record.Err)
	}
	return builder.String()
}

func stdoutWriter(line string) { fmt.Fprintln(os.Stdout, line) }
