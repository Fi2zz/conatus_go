package core_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

func TestReactorBroadcastsToListener(t *testing.T) {
	reactor := core.NewReactor(core.DefaultMaxRounds)
	count := 0
	reactor.Add(func() { count++ })

	if err := reactor.Notify(); err != nil {
		t.Fatalf("广播不应报错：%v", err)
	}
	if count != 1 {
		t.Fatalf("监听器被调用 %d 次，期望 1 次", count)
	}
}

func TestReactorBroadcastsInRegistrationOrder(t *testing.T) {
	reactor := core.NewReactor(core.DefaultMaxRounds)
	var order []string
	reactor.Add(func() { order = append(order, "a") })
	reactor.Add(func() { order = append(order, "b") })

	_ = reactor.Notify()

	if want := []string{"a", "b"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("调用顺序 = %v，期望 %v", order, want)
	}
}

func TestReactorRerunsRoundOnReentrantNotify(t *testing.T) {
	reactor := core.NewReactor(core.DefaultMaxRounds)
	count := 0
	reactor.Add(func() {
		count++
		if count == 1 {
			_ = reactor.Notify()
		}
	})

	_ = reactor.Notify()

	if count != 2 {
		t.Fatalf("重入应触发额外一轮，监听器被调用 %d 次，期望 2 次", count)
	}
}

func TestReactorHandleRemoveStopsDelivery(t *testing.T) {
	reactor := core.NewReactor(core.DefaultMaxRounds)
	count := 0
	handle := reactor.Add(func() { count++ })

	if !handle.Remove() {
		t.Fatal("首次 Remove 应返回 true")
	}
	if handle.Remove() {
		t.Fatal("重复 Remove 应返回 false")
	}
	_ = reactor.Notify()

	if count != 0 {
		t.Fatalf("移除后监听器仍被调用 %d 次", count)
	}
}

func TestReactorRemovesOnlyOneDuplicateListener(t *testing.T) {
	reactor := core.NewReactor(core.DefaultMaxRounds)
	count := 0
	listener := func() { count++ }
	first := reactor.Add(listener)
	_ = reactor.Add(listener)

	if !first.Remove() {
		t.Fatal("移除重复监听器中的一个应成功")
	}
	_ = reactor.Notify()

	if count != 1 {
		t.Fatalf("监听器被调用 %d 次，期望 1 次（只移除一个实例）", count)
	}
	if reactor.Len() != 1 {
		t.Fatalf("剩余监听器数 = %d，期望 1", reactor.Len())
	}
}

func TestReactorReportsNonConvergence(t *testing.T) {
	reactor := core.NewReactor(5)
	reactor.Add(func() { _ = reactor.Notify() })

	err := reactor.Notify()

	if !errors.Is(err, core.ErrNotConverged) {
		t.Fatalf("未收敛应返回 ErrNotConverged，得到 %v", err)
	}
}

func TestReactorIsReusableAfterNonConvergence(t *testing.T) {
	reactor := core.NewReactor(3)
	bad := reactor.Add(func() { _ = reactor.Notify() })

	if err := reactor.Notify(); !errors.Is(err, core.ErrNotConverged) {
		t.Fatalf("期望未收敛错误，得到 %v", err)
	}

	bad.Remove()
	count := 0
	reactor.Add(func() { count++ })
	if err := reactor.Notify(); err != nil {
		t.Fatalf("复用后广播不应报错：%v", err)
	}
	if count != 1 {
		t.Fatalf("复用后监听器被调用 %d 次，期望 1 次", count)
	}
}

func TestReactorRejectsNonPositiveMaxRounds(t *testing.T) {
	assertPanics(t, "NewReactor(0)", func() { core.NewReactor(0) })
	assertPanics(t, "NewReactor(-1)", func() { core.NewReactor(-1) })
}

func TestReactorAddRejectsNilListener(t *testing.T) {
	reactor := core.NewReactor(core.DefaultMaxRounds)
	assertPanics(t, "Add(nil)", func() { reactor.Add(nil) })
}

func TestReactorReportsRunningDuringBroadcast(t *testing.T) {
	reactor := core.NewReactor(core.DefaultMaxRounds)
	var during bool
	reactor.Add(func() { during = reactor.Running() })

	_ = reactor.Notify()

	if !during {
		t.Fatal("广播期间 Running 应为 true")
	}
	if reactor.Running() {
		t.Fatal("广播结束后 Running 应为 false")
	}
}

func TestReactorToleratesListenerListMutation(t *testing.T) {
	reactor := core.NewReactor(core.DefaultMaxRounds)
	extra := reactor.Add(func() {})
	reactor.Add(func() { extra.Remove() })

	if err := reactor.Notify(); err != nil {
		t.Fatalf("回调中增删监听器不应报错：%v", err)
	}
	if reactor.Len() != 1 {
		t.Fatalf("剩余监听器数 = %d，期望 1", reactor.Len())
	}
}

func TestReactorRemovesLaterListenerWithoutTouchingEarlier(t *testing.T) {
	reactor := core.NewReactor(core.DefaultMaxRounds)
	first, second := 0, 0
	reactor.Add(func() { first++ })
	later := reactor.Add(func() { second++ })

	if !later.Remove() {
		t.Fatal("移除后加入的监听器应成功")
	}
	_ = reactor.Notify()

	if first != 1 || second != 0 {
		t.Fatalf("调用次数 = (first %d, second %d)，期望 (1, 0)", first, second)
	}
}

func TestReactorReportsMaxRounds(t *testing.T) {
	if got := core.NewReactor(7).MaxRounds(); got != 7 {
		t.Fatalf("MaxRounds = %d，期望 7", got)
	}
	if got := core.NewReactor(core.DefaultMaxRounds).MaxRounds(); got != core.DefaultMaxRounds {
		t.Fatalf("MaxRounds = %d，期望 %d", got, core.DefaultMaxRounds)
	}
}

func TestNilListenerHandleRemoveIsSafe(t *testing.T) {
	var handle *core.ListenerHandle
	if handle.Remove() {
		t.Fatal("nil 句柄的 Remove 应返回 false 而不是 panic")
	}
}
