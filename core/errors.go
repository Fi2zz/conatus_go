package core

import (
	"fmt"
	"os"
	"sync"
)

// errorSink 是根上下文持有、整棵树共享的错误接收器。
//
// 它承接那些**无法同步冒泡**的错误：inject 回调在广播期间出错时，调用栈已经断开，
// 只能交给「环境」。这正是 Dart 版交给 `Zone.current.handleUncaughtError` 的位置，
// 区别仅在于 Dart 的 Zone 是环境隐式的，Go 侧显式挂在上下文树上。
type errorSink struct {
	mu      sync.Mutex
	handler func(error)
}

func newErrorSink() *errorSink {
	return &errorSink{handler: writeErrorToStderr}
}

func (s *errorSink) report(err error) {
	s.mu.Lock()
	handler := s.handler
	s.mu.Unlock()
	handler(err)
}

func (s *errorSink) set(handler func(error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if handler == nil {
		handler = writeErrorToStderr
	}
	s.handler = handler
}

func writeErrorToStderr(err error) {
	fmt.Fprintf(os.Stderr, "conatus: %v\n", err)
}
