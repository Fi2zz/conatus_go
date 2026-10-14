package timer

import (
	"sync"
	"time"

	"github.com/Fi2zz/conatus_go/core"
)

// Debounced 是防抖后的可调用包装：最后一次 [Debounced.Call] 之后静默 delay
// 才执行 callback。
type Debounced struct {
	callback func()
	delay    time.Duration

	mu       sync.Mutex
	handle   *time.Timer
	disposed bool
}

// NewDebounced 创建防抖包装。
func NewDebounced(callback func(), delay time.Duration) *Debounced {
	return &Debounced{callback: callback, delay: delay}
}

// Debounce 在上下文上创建防抖包装：上下文释放时自动停用。
func Debounce(ctx *core.Context, callback func(), delay time.Duration) *Debounced {
	debounced := NewDebounced(callback, delay)
	ctx.Track(debounced.Dispose)
	return debounced
}

// Call 触发一次调用（会重置计时）。
func (d *Debounced) Call() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.disposed {
		return
	}
	if d.handle != nil {
		d.handle.Stop()
	}
	d.handle = time.AfterFunc(d.delay, d.fire)
}

// Dispose 取消挂起中的执行并停用该包装（幂等）。
func (d *Debounced) Dispose() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.disposed {
		return nil
	}
	d.disposed = true
	if d.handle != nil {
		d.handle.Stop()
		d.handle = nil
	}
	return nil
}

func (d *Debounced) fire() {
	d.mu.Lock()
	if d.disposed {
		d.mu.Unlock()
		return
	}
	d.handle = nil
	d.mu.Unlock()
	d.callback()
}
