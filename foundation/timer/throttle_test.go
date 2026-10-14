package timer_test

import (
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/timer"
)

func TestThrottleRunsOncePerWindowWithTrailing(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	fired := &counter{}

	throttled := timer.Throttle(ctx, fired.inc, 40*time.Millisecond)
	throttled.Call()
	throttled.Call()
	throttled.Call()
	if fired.get() != 1 {
		t.Fatalf("窗口内执行次数 = %d，期望 1", fired.get())
	}

	time.Sleep(90 * time.Millisecond)
	if fired.get() != 2 {
		t.Fatalf("窗口结束时应补执行一次，执行次数 = %d，期望 2", fired.get())
	}
}

func TestThrottleDropsCallsWhenTrailingDisabled(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	fired := &counter{}

	throttled := timer.Throttle(ctx, fired.inc, 40*time.Millisecond,
		timer.WithTrailing(false))
	throttled.Call()
	throttled.Call()
	if fired.get() != 1 {
		t.Fatalf("窗口内执行次数 = %d，期望 1", fired.get())
	}

	time.Sleep(90 * time.Millisecond)
	if fired.get() != 1 {
		t.Fatalf("trailing=false 时不应补执行，执行次数 = %d，期望 1", fired.get())
	}
}

// TestThrottleWindowUsesInjectedClock 用注入时钟把「窗口」判定确定化：不依赖真实等待。
func TestThrottleWindowUsesInjectedClock(t *testing.T) {
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	fired := &counter{}
	throttled := timer.NewThrottled(fired.inc, time.Minute,
		timer.WithThrottleClock(func() time.Time { return now }),
		timer.WithTrailing(false))

	throttled.Call()
	if fired.get() != 1 {
		t.Fatalf("首次调用应执行，执行次数 = %d", fired.get())
	}

	now = now.Add(30 * time.Second)
	throttled.Call()
	if fired.get() != 1 {
		t.Fatalf("窗口内调用应被丢弃，执行次数 = %d，期望 1", fired.get())
	}

	now = now.Add(31 * time.Second)
	throttled.Call()
	if fired.get() != 2 {
		t.Fatalf("窗口过后应再次执行，执行次数 = %d，期望 2", fired.get())
	}
}

func TestThrottleDisposedByContextRelease(t *testing.T) {
	ctx := core.NewRoot("root")
	fired := &counter{}

	throttled := timer.Throttle(ctx, fired.inc, 30*time.Millisecond)
	throttled.Call()
	ctx.Dispose()

	time.Sleep(80 * time.Millisecond)
	if fired.get() != 1 {
		t.Fatalf("上下文释放应撤销挂起中的补执行，执行次数 = %d，期望 1", fired.get())
	}

	throttled.Call()
	if fired.get() != 1 {
		t.Fatal("停用后的包装不应再执行")
	}
}

func TestDebounceRunsOnceAfterQuietPeriod(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	fired := &counter{}

	debounced := timer.Debounce(ctx, fired.inc, 30*time.Millisecond)
	debounced.Call()
	debounced.Call()
	debounced.Call()
	if fired.get() != 0 {
		t.Fatalf("静默期未过不应执行，执行次数 = %d", fired.get())
	}

	time.Sleep(80 * time.Millisecond)
	if fired.get() != 1 {
		t.Fatalf("静默期后应执行一次，执行次数 = %d，期望 1", fired.get())
	}
}

func TestDebounceDisposedByContextRelease(t *testing.T) {
	ctx := core.NewRoot("root")
	fired := &counter{}

	debounced := timer.Debounce(ctx, fired.inc, 30*time.Millisecond)
	debounced.Call()
	ctx.Dispose()

	time.Sleep(80 * time.Millisecond)
	if fired.get() != 0 {
		t.Fatalf("上下文释放应取消挂起中的执行，执行次数 = %d，期望 0", fired.get())
	}
}
