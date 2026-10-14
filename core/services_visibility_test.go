package core_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

// 服务可见性：沿父链向下可见，子级服务对父级不可见——这是分层作用域的必然结果，
// 也是「插件之间不互相污染」的保证。
func TestProvideGetHas(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	if ctx.Has("foo") {
		t.Fatal("未提供的服务不应可见")
	}
	if _, found := core.Get[string](ctx, "foo"); found {
		t.Fatal("未提供的服务不应查得到")
	}

	if _, err := ctx.Provide("foo", "bar"); err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	if !ctx.Has("foo") {
		t.Fatal("提供的服务应可见")
	}
	value, found := core.Get[string](ctx, "foo")
	if !found || value != "bar" {
		t.Fatalf("get = (%q, %v)，期望 (bar, true)", value, found)
	}
}

func TestRequireReportsMissingService(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	_, err := core.Require[string](ctx, "foo")
	if err == nil {
		t.Fatal("缺失服务应返回错误")
	}
	if !strings.Contains(err.Error(), "不可用") {
		t.Fatalf("错误信息应说明服务不可用，得到 %v", err)
	}
}

func TestProvideRejectsDuplicateKey(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	if _, err := ctx.Provide("foo", "bar"); err != nil {
		t.Fatalf("首次 provide 失败：%v", err)
	}
	if _, err := ctx.Provide("foo", "baz"); err == nil {
		t.Fatal("重复 provide 应返回错误")
	}
}

func TestProvideRejectsNilValue(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	if _, err := ctx.Provide("foo", nil); err == nil {
		t.Fatal("nil 服务值应被拒绝（Dart 版允许但取不回来）")
	}
	if ctx.Has("foo") {
		t.Fatal("被拒绝的 provide 不应留下痕迹")
	}
}

func TestProvideRejectsDisposedContext(t *testing.T) {
	ctx := core.NewRoot("root")
	ctx.Dispose()

	if _, err := ctx.Provide("foo", "bar"); err == nil {
		t.Fatal("已释放的上下文不应再能提供服务")
	}
}

func TestChildInheritsParentService(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)
	if _, err := root.Provide("foo", "bar"); err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	child, err := root.Plugin("child", func(*core.Context) error { return nil })
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	if !child.Has("foo") {
		t.Fatal("子上下文应继承父上下文服务")
	}
	value, found := core.Get[string](child, "foo")
	if !found || value != "bar" {
		t.Fatalf("子上下文 get = (%q, %v)，期望 (bar, true)", value, found)
	}
}

func TestChildServiceShadowsParent(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)
	if _, err := root.Provide("foo", "root"); err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	child, err := root.Plugin("child", func(ctx *core.Context) error {
		_, provideErr := ctx.Provide("foo", "child")
		return provideErr
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	childValue, _ := core.Get[string](child, "foo")
	rootValue, _ := core.Get[string](root, "foo")
	if childValue != "child" || rootValue != "root" {
		t.Fatalf("遮蔽结果 = (child %q, root %q)，期望 (child, root)", childValue, rootValue)
	}
}

func TestChildServiceInvisibleToParent(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)

	_, err := root.Plugin("child", func(ctx *core.Context) error {
		_, provideErr := ctx.Provide("foo", "bar")
		return provideErr
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	if root.Has("foo") {
		t.Fatal("子上下文提供的服务对父级必须不可见")
	}
}

func TestProvidedDisposerRemovesServiceEarly(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	stop, err := ctx.Provide("foo", "bar")
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}
	if !ctx.Has("foo") {
		t.Fatal("provide 后服务应可见")
	}

	if err := stop(); err != nil {
		t.Fatalf("撤销服务失败：%v", err)
	}
	if ctx.Has("foo") {
		t.Fatal("撤销后服务应不可见")
	}
	if err := stop(); err != nil {
		t.Fatalf("重复撤销应幂等且不报错：%v", err)
	}
}

func TestLocalServiceKeysOnlyListsDirectServices(t *testing.T) {
	root := core.NewRoot("root")
	t.Cleanup(root.Dispose)
	if _, err := root.Provide("foo", 1); err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	child, err := root.Plugin("child", func(ctx *core.Context) error {
		_, provideErr := ctx.Provide("bar", 2)
		return provideErr
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	if want := []string{"foo"}; !reflect.DeepEqual(root.LocalServiceKeys(), want) {
		t.Fatalf("root 直接服务 = %v，期望 %v", root.LocalServiceKeys(), want)
	}
	if want := []string{"bar"}; !reflect.DeepEqual(child.LocalServiceKeys(), want) {
		t.Fatalf("child 直接服务 = %v，期望 %v", child.LocalServiceKeys(), want)
	}
}
