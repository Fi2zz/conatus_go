package session

import (
	"sync"

	"github.com/Fi2zz/conatus_go/core"
)

type eventSink struct{ fn func(Event) }

type closeSink struct{ fn func() }

// OnEvent 监听后续追加，返回撤销函数（幂等）。会话已关闭时登记无效（不会再触发）。
func (s *Session) OnEvent(listener func(Event)) core.Disposer {
	handle := &eventSink{fn: listener}
	s.mu.Lock()
	if !s.closedFlag.Load() {
		s.eventSinks = append(s.eventSinks, handle)
	}
	s.mu.Unlock()

	var once sync.Once
	return func() error {
		once.Do(func() {
			s.mu.Lock()
			s.eventSinks = removeSink(s.eventSinks, handle)
			s.mu.Unlock()
		})
		return nil
	}
}

// OnClose 监听会话关闭；已关闭时立即回调。返回撤销函数（幂等）。
func (s *Session) OnClose(listener func()) core.Disposer {
	handle := &closeSink{fn: listener}
	s.mu.Lock()
	registerNow := s.closedFlag.Load()
	if !registerNow {
		s.closeSinks = append(s.closeSinks, handle)
	}
	s.mu.Unlock()

	if registerNow {
		listener() // 已关闭：立即回调，且不在锁内调用
	}

	var once sync.Once
	return func() error {
		once.Do(func() {
			s.mu.Lock()
			s.closeSinks = removeSink(s.closeSinks, handle)
			s.mu.Unlock()
		})
		return nil
	}
}

// Close 关闭会话：通知关闭监听器并拒绝后续追加。幂等。
func (s *Session) Close() {
	if !s.closedFlag.CompareAndSwap(false, true) {
		return
	}
	s.mu.Lock()
	sinks := s.closeSinks
	s.closeSinks = nil
	s.eventSinks = nil
	s.mu.Unlock()

	for _, sink := range sinks {
		sink.fn()
	}
}

// removeSink 按句柄移除一个登记（保持其余顺序）。
func removeSink[T any](sinks []*T, target *T) []*T {
	for index, sink := range sinks {
		if sink != target {
			continue
		}
		return append(sinks[:index], sinks[index+1:]...)
	}
	return sinks
}
