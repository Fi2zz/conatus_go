package timer

import (
	"sync"
	"time"

	"github.com/Fi2zz/conatus_go/core"
)

// Throttled 是节流后的可调用包装：调用 [Throttled.Call] 触发一次调用，
// [Throttled.Dispose] 取消挂起中的补执行。
type Throttled struct {
	callback func()
	delay    time.Duration
	trailing bool
	clock    func() time.Time

	mu       sync.Mutex
	handle   *time.Timer
	lastRun  time.Time
	disposed bool
}

// ThrottleOption 定制 [Throttled]。
type ThrottleOption func(*Throttled)

// WithTrailing 控制被抑制的调用是否在窗口结束时补执行一次（缺省补执行）。
func WithTrailing(trailing bool) ThrottleOption {
	return func(t *Throttled) { t.trailing = trailing }
}

// WithThrottleClock 注入时钟（测试用；缺省 time.Now）。
func WithThrottleClock(clock func() time.Time) ThrottleOption {
	return func(t *Throttled) { t.clock = clock }
}

// NewThrottled 创建节流包装。
func NewThrottled(callback func(), delay time.Duration, opts ...ThrottleOption) *Throttled {
	throttled := &Throttled{callback: callback, delay: delay, trailing: true, clock: time.Now}
	for _, opt := range opts {
		opt(throttled)
	}
	return throttled
}

// Throttle 在上下文上创建节流包装：上下文释放时自动停用。
func Throttle(ctx *core.Context, callback func(), delay time.Duration, opts ...ThrottleOption) *Throttled {
	throttled := NewThrottled(callback, delay, opts...)
	ctx.Track(throttled.Dispose)
	return throttled
}

// Call 触发一次调用：delay 窗口内的重复调用最多执行一次。
func (t *Throttled) Call() {
	if t.inactive() {
		return
	}
	t.mu.Lock()
	remaining := t.delay - t.clock().Sub(t.lastRun)
	t.mu.Unlock()

	if remaining <= 0 {
		t.run()
		return
	}
	t.scheduleTrailing(remaining)
}

// Dispose 取消挂起中的定时器并停用该包装（幂等）。
func (t *Throttled) Dispose() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.disposed {
		return nil
	}
	t.disposed = true
	if t.handle != nil {
		t.handle.Stop()
		t.handle = nil
	}
	return nil
}

func (t *Throttled) inactive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.disposed
}

func (t *Throttled) scheduleTrailing(remaining time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.disposed || !t.trailing {
		return
	}
	if t.handle != nil {
		t.handle.Stop()
	}
	t.handle = time.AfterFunc(remaining, t.run)
}

func (t *Throttled) run() {
	t.mu.Lock()
	if t.disposed {
		t.mu.Unlock()
		return
	}
	t.handle = nil
	t.lastRun = t.clock()
	t.mu.Unlock()
	t.callback()
}
