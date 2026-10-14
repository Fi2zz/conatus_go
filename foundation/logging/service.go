package logging

import (
	"sync"
	"time"
)

// ServiceOption 定制 [Service]。
type ServiceOption func(*Service)

// WithDefaultName 设定直接用服务方法记录时使用的名字。
func WithDefaultName(name string) ServiceOption {
	return func(s *Service) { s.defaultName = name }
}

// WithMinLevel 设定全局最小级别；低于它的日志被丢弃。
func WithMinLevel(level Level) ServiceOption {
	return func(s *Service) { s.level = level }
}

// WithRecentLimit 设定 recent 缓冲保留的最大条数。
func WithRecentLimit(limit int) ServiceOption {
	return func(s *Service) { s.recentLimit = limit }
}

// WithClock 注入时钟（测试用；缺省 time.Now）。
func WithClock(clock func() time.Time) ServiceOption {
	return func(s *Service) { s.clock = clock }
}

// Service 是内置日志服务，可被多 goroutine 并发使用。
type Service struct {
	mu          sync.Mutex
	defaultName string
	level       Level
	exporters   []Exporter
	recent      []Record
	recentLimit int
	clock       func() time.Time
}

// NewService 创建一个日志服务。
func NewService(opts ...ServiceOption) *Service {
	service := &Service{defaultName: "root", level: LevelInfo, recentLimit: 100, clock: time.Now}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

// DefaultName 返回直接用服务方法记录时使用的名字。
func (s *Service) DefaultName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.defaultName
}

// MinLevel 返回全局最小级别。
func (s *Service) MinLevel() Level {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.level
}

// SetMinLevel 调整全局最小级别。
func (s *Service) SetMinLevel(level Level) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.level = level
}

// RecentLimit 返回 recent 缓冲上限。
func (s *Service) RecentLimit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recentLimit
}

// SetRecentLimit 调整 recent 缓冲上限。
func (s *Service) SetRecentLimit(limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recentLimit = limit
}

// Recent 返回最近记录的日志（最多 RecentLimit 条）的副本。
func (s *Service) Recent() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Record(nil), s.recent...)
}

// Exporters 返回已登记的导出器快照。
func (s *Service) Exporters() []Exporter {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Exporter(nil), s.exporters...)
}

// Logger 创建一个命名 logger；name 为空时用 DefaultName。
func (s *Service) Logger(name string) *Logger {
	if name == "" {
		name = s.DefaultName()
	}
	return &Logger{name: name, service: s}
}

// AddExporter 登记一个导出器。
func (s *Service) AddExporter(exporter Exporter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exporters = append(s.exporters, exporter)
}

// RemoveExporter 移除一个导出器，返回是否确实移除了一个实例（幂等）。
func (s *Service) RemoveExporter(exporter Exporter) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, registered := range s.exporters {
		if registered != exporter {
			continue
		}
		s.exporters = append(s.exporters[:index], s.exporters[index+1:]...)
		return true
	}
	return false
}

// Debug 以 defaultName 记录 debug 级日志。
func (s *Service) Debug(message any, opts ...EntryOption) {
	s.emitAs(LevelDebug, s.DefaultName(), message, opts...)
}

// Info 以 defaultName 记录 info 级日志。
func (s *Service) Info(message any, opts ...EntryOption) {
	s.emitAs(LevelInfo, s.DefaultName(), message, opts...)
}

// Warn 以 defaultName 记录 warn 级日志。
func (s *Service) Warn(message any, opts ...EntryOption) {
	s.emitAs(LevelWarn, s.DefaultName(), message, opts...)
}

// Error 以 defaultName 记录 error 级日志。
func (s *Service) Error(message any, opts ...EntryOption) {
	s.emitAs(LevelError, s.DefaultName(), message, opts...)
}

// emitAs 是唯一的落点：级别过滤 → 入 recent 缓冲 → 派发导出器。
//
// 导出器在锁外调用，避免导出器回头打日志时死锁。
func (s *Service) emitAs(level Level, name string, message any, opts ...EntryOption) {
	extra := entry{}
	for _, opt := range opts {
		opt(&extra)
	}

	s.mu.Lock()
	if level < s.level {
		s.mu.Unlock()
		return
	}
	record := Record{
		Level:   level,
		Name:    name,
		Message: message,
		Time:    s.clock(),
		Err:     extra.err,
		Stack:   extra.stack,
	}
	s.recent = append(s.recent, record)
	if overflow := len(s.recent) - s.recentLimit; overflow > 0 {
		s.recent = append(s.recent[:0], s.recent[overflow:]...)
	}
	exporters := append([]Exporter(nil), s.exporters...)
	s.mu.Unlock()

	for _, exporter := range exporters {
		exporter.Export(record)
	}
}
