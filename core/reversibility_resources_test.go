package core_test

import (
	"sync/atomic"
	"time"
)

// resourceHandle 是一个外部资源句柄（真资源探针里对应一个真实文件句柄）。
type resourceHandle struct {
	id   string
	open bool
}

// Close 关闭句柄，即该效果的逆。
func (h *resourceHandle) Close() { h.open = false }

// timerHandle 是周期定时器。
//
// Go 的 time.Ticker 没有 Dart `Timer.isActive` 那样的存活查询，因此存活位由世界
// 自己记；同时保留一个真实 tick 计数与一个后台 goroutine——卸载后 tick 必须停止
// 增长、goroutine 必须退出，这两条是独立于存活位的硬证据。
type timerHandle struct {
	ticker  *time.Ticker
	done    chan struct{}
	stopped atomic.Bool
	ticks   atomic.Int64
}

func newTimerHandle() *timerHandle {
	handle := &timerHandle{ticker: time.NewTicker(5 * time.Millisecond), done: make(chan struct{})}
	go handle.run()
	return handle
}

func (h *timerHandle) run() {
	for {
		select {
		case <-h.done:
			return
		case <-h.ticker.C:
			h.ticks.Add(1)
		}
	}
}

// Cancel 停止定时器并结束它的 goroutine（幂等），即该效果的逆。
func (h *timerHandle) Cancel() {
	if h.stopped.Swap(true) {
		return
	}
	h.ticker.Stop()
	close(h.done)
}

// Active 返回定时器是否仍然存活。
func (h *timerHandle) Active() bool { return !h.stopped.Load() }

// Ticks 返回真实触发过的周期回调次数。
func (h *timerHandle) Ticks() int64 { return h.ticks.Load() }

// streamHandle 是一条订阅：后台 goroutine 阻塞在信号通道上，取消即关闭信号，
// goroutine 随之退出（对应 Dart 版 `StreamController.hasListener` 的位置）。
type streamHandle struct {
	signal   chan struct{}
	finished chan struct{}
	closed   atomic.Bool
}

func newStreamHandle() *streamHandle {
	handle := &streamHandle{signal: make(chan struct{}), finished: make(chan struct{})}
	go func() {
		defer close(handle.finished)
		<-handle.signal
	}()
	return handle
}

// Cancel 取消订阅（幂等），即该效果的逆。
func (h *streamHandle) Cancel() {
	if h.closed.Swap(true) {
		return
	}
	close(h.signal)
}

// Active 返回订阅是否仍然存活。
func (h *streamHandle) Active() bool { return !h.closed.Load() }

// Finished 在订阅的后台 goroutine 真正退出后关闭。
func (h *streamHandle) Finished() <-chan struct{} { return h.finished }
