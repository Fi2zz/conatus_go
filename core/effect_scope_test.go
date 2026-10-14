package core_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

func TestEffectScopeDisposesInLIFOOrder(t *testing.T) {
	scope := core.NewEffectScope()
	var order []int
	for _, value := range []int{1, 2, 3} {
		scope.Track(core.Ignore(func() { order = append(order, value) }))
	}

	if errs := scope.Dispose(); len(errs) != 0 {
		t.Fatalf("无错撤销不应产生错误：%v", errs)
	}
	if want := []int{3, 2, 1}; !reflect.DeepEqual(order, want) {
		t.Fatalf("撤销顺序 = %v，期望 %v", order, want)
	}
}

func TestEffectScopeDisposeIsIdempotent(t *testing.T) {
	scope := core.NewEffectScope()
	count := 0
	scope.Track(core.Ignore(func() { count++ }))

	scope.Dispose()
	scope.Dispose()
	scope.Dispose()

	if count != 1 {
		t.Fatalf("撤销被执行 %d 次，期望 1 次", count)
	}
}

func TestEffectScopeTrackAfterDisposeRunsImmediately(t *testing.T) {
	scope := core.NewEffectScope()
	scope.Dispose()

	called := false
	scope.Track(core.Ignore(func() { called = true }))

	if !called {
		t.Fatal("释放后登记的撤销函数应被立即执行")
	}
}

func TestEffectScopeTrackRejectsNilDisposer(t *testing.T) {
	scope := core.NewEffectScope()
	assertPanics(t, "Track(nil)", func() { scope.Track(nil) })
}

func TestEffectScopeContinuesAfterFailingDisposer(t *testing.T) {
	scope := core.NewEffectScope()
	var order []int
	scope.Track(core.Ignore(func() { order = append(order, 1) }))
	scope.Track(func() error { return errors.New("boom") })
	scope.Track(core.Ignore(func() { order = append(order, 3) }))

	errs := scope.Dispose()

	if want := []int{3, 1}; !reflect.DeepEqual(order, want) {
		t.Fatalf("撤销顺序 = %v，期望 %v（出错的逆不阻断其余）", order, want)
	}
	if len(errs) != 1 {
		t.Fatalf("聚合错误数 = %d，期望 1", len(errs))
	}
	if errs[0].Error() != "boom" {
		t.Fatalf("错误内容 = %v，期望 boom", errs[0])
	}
}

func TestEffectScopeContinuesAfterPanickingDisposer(t *testing.T) {
	scope := core.NewEffectScope()
	var order []int
	scope.Track(core.Ignore(func() { order = append(order, 1) }))
	scope.Track(func() error { panic("爆炸") })
	scope.Track(core.Ignore(func() { order = append(order, 3) }))

	errs := scope.Dispose()

	if want := []int{3, 1}; !reflect.DeepEqual(order, want) {
		t.Fatalf("撤销顺序 = %v，期望 %v（panic 的逆不阻断其余）", order, want)
	}
	if len(errs) != 1 {
		t.Fatalf("聚合错误数 = %d，期望 1（panic 转成错误返回）", len(errs))
	}
}

func TestEffectScopeCaptureTracksReturnedDisposer(t *testing.T) {
	scope := core.NewEffectScope()
	disposed := false

	returned := scope.Capture(func() core.Disposer {
		return core.Ignore(func() { disposed = true })
	})

	if disposed {
		t.Fatal("登记阶段不应执行撤销函数")
	}
	if scope.Len() != 1 {
		t.Fatalf("capture 未登记返回值：Len = %d", scope.Len())
	}
	scope.Dispose()
	if !disposed {
		t.Fatal("dispose 后撤销函数应已执行")
	}
	if returned == nil {
		t.Fatal("capture 应把 body 的返回值原样返回")
	}
}

func TestEffectScopeRunDoesNotRegister(t *testing.T) {
	scope := core.NewEffectScope()
	ran := false

	scope.Run(func() { ran = true })

	if !ran {
		t.Fatal("Run 应执行 body")
	}
	if scope.Len() != 0 {
		t.Fatalf("Run 不应登记任何撤销函数：Len = %d", scope.Len())
	}
}

func TestEffectScopeReportsDisposedAndLength(t *testing.T) {
	scope := core.NewEffectScope()
	if scope.Disposed() {
		t.Fatal("新建作用域不应是已释放状态")
	}
	if scope.Len() != 0 {
		t.Fatalf("新建作用域长度应为 0，得到 %d", scope.Len())
	}

	scope.Track(core.Ignore(func() {}))
	scope.Track(core.Ignore(func() {}))
	if scope.Len() != 2 {
		t.Fatalf("登记两个撤销后长度应为 2，得到 %d", scope.Len())
	}

	scope.Dispose()
	if !scope.Disposed() {
		t.Fatal("dispose 后应为已释放状态")
	}
	if scope.Len() != 0 {
		t.Fatalf("dispose 后长度应归零，得到 %d", scope.Len())
	}
}
