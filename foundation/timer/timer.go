// Package timer 把定时器做成 [core.Context] 的可逆效应（对应 Dart 的 timer 插件）。
//
// 所有定时器都登记在调用方上下文的 EffectScope 上，随上下文释放自动清理；也可通过
// 返回的 Disposer 提前取消。
//
// 与 Dart 版的差别：那边是扩展方法（`ctx.timeout(...)`），Go 侧是自由函数
// （`timer.Timeout(ctx, ...)`）——Go 不能给已有类型加方法。
//
// 回调在定时器自己的 goroutine 里执行：回调内 panic 会终止进程（与 Go 其它 goroutine
// 一致），需要在回调里抗异常时自行 recover。
package timer

import (
	"fmt"
	"sync"
	"time"

	"github.com/Fi2zz/conatus_go/core"
)

// Timeout 延迟 delay 后执行一次 callback；返回的 Disposer 可提前取消。
func Timeout(ctx *core.Context, callback func(), delay time.Duration) core.Disposer {
	return ctx.Effect(func() core.Disposer {
		handle := time.AfterFunc(delay, callback)
		return core.Ignore(func() { handle.Stop() })
	})
}

// Interval 每 delay 执行一次 callback；返回的 Disposer 可提前取消。
func Interval(ctx *core.Context, callback func(), delay time.Duration) core.Disposer {
	return ctx.Effect(func() core.Disposer {
		ticker := time.NewTicker(delay)
		stop := make(chan struct{})
		go func() {
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					callback()
				}
			}
		}()
		var once sync.Once
		return func() error {
			once.Do(func() {
				ticker.Stop()
				close(stop)
			})
			return nil
		}
	})
}

// Sleep 等待 delay。
//
// 若上下文在到点前被释放，返回错误而不是永远挂住调用方。
func Sleep(ctx *core.Context, delay time.Duration) error {
	fired := make(chan struct{})
	interrupted := make(chan struct{})
	handle := time.AfterFunc(delay, func() { close(fired) })
	ctx.Track(func() error {
		if handle.Stop() {
			close(interrupted)
		}
		return nil
	})

	select {
	case <-fired:
		return nil
	case <-interrupted:
		return fmt.Errorf("上下文 %q 已释放，Sleep 被中断", ctx.Name())
	}
}
