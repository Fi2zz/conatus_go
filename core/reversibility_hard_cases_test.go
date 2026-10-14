package core_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

// 对抗探针：把验证期间证明有效的攻击固定为永久用例。
//
// 前两条是**泄漏检测的端到端钉**（只泄漏一种资源，确保投影的每一行都被独立钉住）；
// 后两条是机制没直接覆盖的失败模式（嵌套级联、逆失败）。
func TestPlaceboInverseLeakingStreamIsDetected(t *testing.T) {
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)

	problems := harness.runLifo([]pluginSpec{
		newPluginSpecWithMode("p", modePlacebo, effectOpenStream),
	})

	if len(problems) == 0 {
		t.Fatal("只泄漏订阅的安慰剂逆必须被检出")
	}
}

func TestPlaceboInverseLeakingHandleIsDetected(t *testing.T) {
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)

	problems := harness.runLifo([]pluginSpec{
		newPluginSpecWithMode("p", modePlacebo, effectAcquireHandle),
	})

	if len(problems) == 0 {
		t.Fatal("只泄漏句柄的安慰剂逆必须被检出")
	}
}

func TestNestedPluginUnmountRestoresBaseline(t *testing.T) {
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)
	baseline := harness.project()

	outer, err := harness.root.Plugin("outer", func(c *core.Context) error {
		_, innerErr := c.Plugin("inner", func(inner *core.Context) error {
			inner.Track(applyEffect(harness.world, inner, effectRegisterName))
			inner.Track(applyEffect(harness.world, inner, effectBumpShared))
			return nil
		})
		return innerErr
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	if reflect.DeepEqual(harness.project(), baseline) {
		t.Fatal("嵌套挂载后投影应变化")
	}

	outer.Dispose()

	if !reflect.DeepEqual(harness.project(), baseline) {
		t.Fatalf("卸载外层后应回到基线：\n基线 %v\n实际 %v", baseline, harness.project())
	}
	if len(harness.world.registry) != 0 {
		t.Fatalf("注册表应被清空：%v", harness.world.registry)
	}
	if len(harness.world.shared) != 0 {
		t.Fatalf("共享值应被清空：%v", harness.world.shared)
	}
}

func TestFailingInverseDoesNotBlockOthers(t *testing.T) {
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)
	var reported []error
	harness.root.SetErrorHandler(func(err error) { reported = append(reported, err) })
	baseline := harness.project()

	ctx, err := harness.root.Plugin("p", func(c *core.Context) error {
		c.Track(applyEffect(harness.world, c, effectRegisterName))
		c.Track(func() error { return errors.New("boom") })
		c.Track(applyEffect(harness.world, c, effectBumpShared))
		return nil
	})
	if err != nil {
		t.Fatalf("plugin 失败：%v", err)
	}

	ctx.Dispose()

	if len(reported) != 1 {
		t.Fatalf("逆失败应上报 1 条错误（dispose 不传播），得到 %d 条", len(reported))
	}
	if !reflect.DeepEqual(harness.project(), baseline) {
		t.Fatalf("一个逆失败不应阻断其余：\n基线 %v\n实际 %v", baseline, harness.project())
	}
	if len(harness.world.registry) != 0 || len(harness.world.shared) != 0 {
		t.Fatal("其余效果应仍被还原")
	}
}
