package timer_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/timer"
)

// counter 是回调里累加的计数器。
//
// 定时器回调在**另一个 goroutine** 上执行（Dart 侧是单线程事件循环，不会有这个
// 差异），因此测试里必须用原子量，否则 -race 会（正确地）报数据竞争。
type counter struct{ value atomic.Int64 }

func (c *counter) inc() { c.value.Add(1) }

func (c *counter) get() int64 { return c.value.Load() }

func TestTimeoutFiresOnce(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	fired := &counter{}

	timer.Timeout(ctx, fired.inc, 20*time.Millisecond)

	if fired.get() != 0 {
		t.Fatal("不应立即执行")
	}
	time.Sleep(80 * time.Millisecond)
	if fired.get() != 1 {
		t.Fatalf("执行次数 = %d，期望 1", fired.get())
	}
}

func TestTimeoutDisposerCancelsEarly(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	fired := &counter{}

	stop := timer.Timeout(ctx, fired.inc, 30*time.Millisecond)
	if err := stop(); err != nil {
		t.Fatalf("提前取消失败：%v", err)
	}

	time.Sleep(70 * time.Millisecond)
	if fired.get() != 0 {
		t.Fatal("提前取消后不应执行")
	}
}

func TestContextDisposeCancelsPendingTimeout(t *testing.T) {
	ctx := core.NewRoot("root")
	fired := &counter{}

	timer.Timeout(ctx, fired.inc, 30*time.Millisecond)
	ctx.Dispose()

	time.Sleep(70 * time.Millisecond)
	if fired.get() != 0 {
		t.Fatal("上下文释放应取消未触发的定时器")
	}
}

func TestIntervalRepeatsUntilDispose(t *testing.T) {
	ctx := core.NewRoot("root")
	fired := &counter{}

	timer.Interval(ctx, fired.inc, 15*time.Millisecond)
	time.Sleep(80 * time.Millisecond)
	if fired.get() < 2 {
		t.Fatalf("触发次数 = %d，期望至少 2", fired.get())
	}

	ctx.Dispose()
	frozen := fired.get()
	time.Sleep(50 * time.Millisecond)
	if fired.get() != frozen {
		t.Fatalf("释放后仍在触发：%d → %d", frozen, fired.get())
	}
}

func TestIntervalDisposerIsIdempotent(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	fired := &counter{}

	stop := timer.Interval(ctx, fired.inc, 15*time.Millisecond)
	time.Sleep(40 * time.Millisecond)

	if err := stop(); err != nil {
		t.Fatalf("首次取消失败：%v", err)
	}
	if err := stop(); err != nil {
		t.Fatalf("重复取消应幂等：%v", err)
	}
	frozen := fired.get()
	time.Sleep(40 * time.Millisecond)
	if fired.get() != frozen {
		t.Fatalf("取消后仍在触发：%d → %d", frozen, fired.get())
	}
}

func TestSleepCompletesOnTime(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	if err := timer.Sleep(ctx, 20*time.Millisecond); err != nil {
		t.Fatalf("正常等待不应报错：%v", err)
	}
	if ctx.Disposed() {
		t.Fatal("等待期间上下文不应被释放")
	}
}

func TestSleepIsInterruptedByDispose(t *testing.T) {
	ctx := core.NewRoot("root")
	result := make(chan error, 1)
	go func() { result <- timer.Sleep(ctx, 5*time.Second) }()

	time.Sleep(20 * time.Millisecond)
	ctx.Dispose()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("上下文释放后 Sleep 应返回错误而不是静默返回")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Sleep 未被打断（调用方悬挂）")
	}
}
