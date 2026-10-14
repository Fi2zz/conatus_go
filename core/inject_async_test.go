package core_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/core"
)

func TestLateTrackAfterAsyncGapRunsImmediately(t *testing.T) {
	ctx := core.NewRoot("root")
	ctx.Dispose()

	var cleaned bool
	time.Sleep(10 * time.Millisecond)
	ctx.Track(core.Ignore(func() { cleaned = true }))

	if !cleaned {
		t.Fatal("异步完成后迟到登记的清理函数应被立即执行")
	}
}

func TestDisposingDuringAsyncInitLeavesNoService(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)

	finished := make(chan struct{})
	plugin, err := root.Plugin("async", func(ctx *core.Context) error {
		go func() {
			defer close(finished)
			time.Sleep(20 * time.Millisecond)
			if ctx.Disposed() {
				return
			}
			_, _ = ctx.Provide("late", 1)
		}()
		return nil
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	plugin.Dispose()
	<-finished

	if plugin.Has("late") {
		t.Fatal("异步初始化期间上下文被释放，服务不应泄漏")
	}
}

func TestConcurrentPluginLifecyclesStayConsistent(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)
	root.SetErrorHandler(func(error) {})

	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			child, err := root.Plugin(fmt.Sprintf("child-%d", index), func(ctx *core.Context) error {
				_, provideErr := ctx.Provide("svc", index)
				return provideErr
			})
			if err != nil {
				t.Errorf("plugin 失败：%v", err)
				return
			}
			child.Dispose()
		}(i)
	}
	group.Wait()

	if len(root.LocalServiceKeys()) != 0 {
		t.Fatalf("根上下文不应残留服务：%v", root.LocalServiceKeys())
	}
	if root.Disposed() {
		t.Fatal("并发挂载卸载不应释放根上下文")
	}
}
