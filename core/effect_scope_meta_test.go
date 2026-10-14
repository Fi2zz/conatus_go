package core_test

import (
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

func TestEffectScopeIDIsAutoAllocatedAndUnique(t *testing.T) {
	a := core.NewEffectScope()
	b := core.NewEffectScope()

	if a.ID() == "" {
		t.Fatal("缺省 id 不应为空")
	}
	if a.ID() == b.ID() {
		t.Fatalf("两个作用域 id 相同：%q", a.ID())
	}
}

func TestEffectScopeIDCanBeSpecified(t *testing.T) {
	scope := core.NewEffectScope(core.WithScopeID("patch-42"))
	if scope.ID() != "patch-42" {
		t.Fatalf("显式 id = %q，期望 patch-42", scope.ID())
	}
}

func TestEffectScopeReversibilityDefaultsToInternal(t *testing.T) {
	if got := core.NewEffectScope().Reversibility(); got != core.ReversibilityInternal {
		t.Fatalf("缺省可逆性 = %v，期望 internal", got)
	}

	oneWay := core.NewEffectScope(core.WithReversibility(core.ReversibilityOneWayDoor))
	if got := oneWay.Reversibility(); got != core.ReversibilityOneWayDoor {
		t.Fatalf("显式可逆性 = %v，期望 oneWayDoor", got)
	}
	if name := core.ReversibilityOneWayDoor.String(); name != "oneWayDoor" {
		t.Fatalf("可逆性名称 = %q，期望 oneWayDoor", name)
	}
}

func TestEffectScopeMetadataDoesNotChangeDisposeOrder(t *testing.T) {
	scope := core.NewEffectScope(
		core.WithScopeID("s"),
		core.WithReversibility(core.ReversibilityCompensable),
	)
	var order []string
	scope.Track(core.Ignore(func() { order = append(order, "a") }))
	scope.Track(core.Ignore(func() { order = append(order, "b") }))

	if errs := scope.Dispose(); len(errs) != 0 {
		t.Fatalf("元数据不应引入错误：%v", errs)
	}
	if want := []string{"b", "a"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("撤销顺序 = %v，期望 %v", order, want)
	}
}

func TestEffectScopeReversibilityNameFallsBackForUnknownValue(t *testing.T) {
	unknown := core.EffectReversibility(99).String()
	if unknown != "EffectReversibility(99)" {
		t.Fatalf("非法枚举值应回退为带数值的名字，得到 %q", unknown)
	}
}
