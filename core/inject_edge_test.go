package core_test

import (
	"errors"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

// inject 的两条边界：回调出错时的错误通道，以及重复依赖的去重。
func TestInjectCallbackErrorIsReportedAndRolledBack(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	var reported []error
	ctx.SetErrorHandler(func(err error) { reported = append(reported, err) })
	mustProvide(t, ctx, "x", 1)

	childDisposed := false
	boom := errors.New("boom")
	ctx.Inject([]string{"x"}, func(child *core.Context) error {
		child.OnDispose(core.Ignore(func() { childDisposed = true }))
		return boom
	})

	if !childDisposed {
		t.Fatal("回调出错后子上下文应被回滚")
	}
	if len(reported) != 1 {
		t.Fatalf("回调错误应上报 1 条（不传播给 Inject 调用者），得到 %d 条", len(reported))
	}
	if !errors.Is(reported[0], boom) {
		t.Fatalf("上报内容 = %v，期望 boom", reported[0])
	}
}

func TestInjectDeduplicatesDependencies(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	mustProvide(t, ctx, "a", 1)

	count := 0
	ctx.Inject([]string{"a", "a", "a"}, func(*core.Context) error {
		count++
		return nil
	})

	if count != 1 {
		t.Fatalf("重复依赖应被去重，激活次数 = %d，期望 1", count)
	}
}

// TestInjectDoesNotReactivateDuringHostDisposal 构造「宿主已在释放过程中、依赖却还
// 齐全」的窗口：此时必须有额外一轮广播（由第二个服务的撤销触发），但绝不能重新激活。
func TestInjectDoesNotReactivateDuringHostDisposal(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)
	mustProvide(t, ctx, "x", 1)

	activations := 0
	ctx.Inject([]string{"x"}, func(*core.Context) error {
		activations++
		return nil
	})
	mustProvide(t, ctx, "y", 2)

	ctx.Dispose()

	if activations != 1 {
		t.Fatalf("宿主释放过程中不应重新激活：激活 %d 次，期望 1 次", activations)
	}
}
