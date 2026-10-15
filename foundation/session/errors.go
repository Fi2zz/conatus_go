package session

import (
	"fmt"
	"os"
	"sync"
)

// errorSink 是落盘错误接收器。
//
// 落盘发生在事件监听器里（在会话锁内），没有错误通道能回到 Append 的调用方，因此
// 错误统一上报给接收器。它用自己的锁，且绝不反向获取会话／仓库锁——这样不会与
// 「仓库锁 → 会话锁」的既有顺序形成环。
type errorSink struct {
	mu      sync.Mutex
	handler func(error)
}

func newErrorSink() *errorSink {
	return &errorSink{
		handler: func(err error) { fmt.Fprintf(os.Stderr, "conatus: 会话落盘失败：%v\n", err) },
	}
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
		return
	}
	s.handler = handler
}
