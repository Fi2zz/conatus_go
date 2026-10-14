package core_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/Fi2zz/conatus_go/core"
)

// bumpPair 是两个都改写同一共享值的插件：只有按逆序还原才回得到基线。
func bumpPair() []pluginSpec {
	return []pluginSpec{
		newPluginSpec("x", effectBumpShared),
		newPluginSpec("y", effectBumpShared),
	}
}

// allKinds 是覆盖全部效果种类的定长序列。
func allKinds() []pluginSpec {
	return []pluginSpec{
		newPluginSpec("a", effectProvideService, effectRegisterName),
		newPluginSpec("b", effectOpenStream, effectStartTimer),
		newPluginSpec("c", effectAcquireHandle, effectBumpShared),
		newPluginSpec("d", effectBumpShared, effectBumpShared),
	}
}

func TestTheorem16ReversesEveryPrefix(t *testing.T) {
	for seed := 1; seed <= 10; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			harness := newReversibilityHarness()
			t.Cleanup(harness.teardown)
			if problems := harness.runLifo(randomPlugins(seed, 5)); len(problems) != 0 {
				t.Fatalf("逆序卸载未逐前缀还原：%v", problems)
			}
		})
	}
}

func TestTheorem16CoversAllEffectKinds(t *testing.T) {
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)

	if problems := harness.runLifo(allKinds()); len(problems) != 0 {
		t.Fatalf("定长序列未逐前缀还原：%v", problems)
	}
}

func TestTheorem16HandlesTwoWritersOfSameValue(t *testing.T) {
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)

	if problems := harness.runLifo(bumpPair()); len(problems) != 0 {
		t.Fatalf("两个改写同一共享值的插件未还原：%v", problems)
	}
}

// TestTheorem7EveryInverseRestoresItsOwnEffect 钉住核心方程 g(f(γ)) = γ：
// 施加效果后手工调用它的逆，状态必须逐字节还原。这条不经过 EffectScope，
// 因此它验的是「逆本身对不对」，与「累积器有没有按 LIFO 调用逆」正交。
func TestTheorem7EveryInverseRestoresItsOwnEffect(t *testing.T) {
	kinds := []effectKind{
		effectBumpShared, effectProvideService, effectOpenStream,
		effectStartTimer, effectAcquireHandle, effectRegisterName,
	}
	for _, kind := range kinds {
		t.Run(fmt.Sprintf("kind=%d", kind), func(t *testing.T) {
			world := newEffectWorld()
			root := core.NewRoot("root")
			t.Cleanup(world.teardown)
			t.Cleanup(root.Dispose)
			plugin, err := root.Plugin("p", func(*core.Context) error { return nil })
			if err != nil {
				t.Fatalf("plugin 失败：%v", err)
			}
			contexts := []*core.Context{root, plugin}

			before := world.project(contexts)
			inverse := applyEffect(world, plugin, kind)

			if reflect.DeepEqual(world.project(contexts), before) {
				t.Fatalf("%v 的效果没有发生", kind)
			}
			if err := inverse(); err != nil {
				t.Fatalf("%v 的逆报错：%v", kind, err)
			}
			if !reflect.DeepEqual(world.project(contexts), before) {
				t.Fatalf("%v 的逆没有还原它自己施加的状态：%v → %v",
					kind, before, world.project(contexts))
			}
		})
	}
}

// TestDefinition8OutOfOrderUnmountDiverges 钉住「逆只在它被施加的状态上见证」：
// oracle 必须对卸载顺序敏感，否则「逆序还原」这个判定是空转的。
func TestDefinition8OutOfOrderUnmountDiverges(t *testing.T) {
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)
	baseline := harness.project()

	result := harness.play(bumpPair(), []int{0, 1})

	if reflect.DeepEqual(result.unmounted[len(result.unmounted)-1], baseline) {
		t.Fatal("乱序卸载必然发散：oracle 对顺序应该敏感")
	}
}

func TestF7ExternalResourcesAreReallyReleased(t *testing.T) {
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)
	baseline := harness.project()

	harness.play(allKinds(), nil)

	if !harness.world.allReleased() {
		t.Fatal("卸载后定时器与订阅应全部释放")
	}
	if open := harness.world.openHandleIDs(); open != "" {
		t.Fatalf("卸载后句柄应全部关闭，仍有 %q", open)
	}
	if !reflect.DeepEqual(harness.project(), baseline) {
		t.Fatalf("卸载后投影未回到基线：\n基线 %v\n实际 %v", baseline, harness.project())
	}
}

func TestF7TwoHundredMountUnmountRoundsDoNotDrift(t *testing.T) {
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)
	baseline := harness.project()
	specs := []pluginSpec{
		newPluginSpec("a", effectStartTimer, effectRegisterName, effectBumpShared),
		newPluginSpec("b", effectOpenStream, effectAcquireHandle),
	}

	for round := 0; round < 200; round++ {
		if problems := harness.runLifo(specs); len(problems) != 0 {
			t.Fatalf("第 %d 轮未还原：%v", round, problems)
		}
	}

	if !reflect.DeepEqual(harness.project(), baseline) {
		t.Fatal("反复挂载卸载后投影发生漂移")
	}
	if !harness.world.allReleased() {
		t.Fatal("反复挂载卸载后仍有存活的定时器或订阅")
	}
}
