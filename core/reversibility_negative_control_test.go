package core_test

import (
	"reflect"
	"testing"
)

// 负向对照：检查器必须先被证明**能失败**，它的通过才有意义。
//
// 三个坏插件分别对应三种真实的假可逆（安慰剂逆 / 漏登记 / 半个逆），本文件断言
// 它们**全部被检出**；同时断言正确插件不被误报、且投影在挂载期间确实变化——
// 后者排除「投影是个常量、所以什么都不报」这种空转的检查器。
func violationsFor(t *testing.T, spec pluginSpec) []string {
	t.Helper()
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)
	return harness.runLifo([]pluginSpec{spec})
}

func TestNegativeControlsDetectFakeReversibility(t *testing.T) {
	cases := []struct {
		name string
		spec pluginSpec
	}{
		{"安慰剂逆（登记了但什么都不做）", newPluginSpecWithMode("p", modePlacebo, effectStartTimer)},
		{"漏登记（逆从未登记）", newPluginSpecWithMode("p", modeUnregistered, effectRegisterName)},
		{"半个逆（只还原一部分）",
			newPluginSpecWithMode("p", modeHalfRestore, effectRegisterName, effectBumpShared)},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if len(violationsFor(t, item.spec)) == 0 {
				t.Fatal("该失败模式必须被检出")
			}
		})
	}
}

// TestHarnessSelfCheckTrackedCounts 钉住三种失败模式确实被构造成了不同形态：
// 它观察的是夹具自己的登记行为，与效果是否泄漏无关。
func TestHarnessSelfCheckTrackedCounts(t *testing.T) {
	cases := []struct {
		mode pluginMode
		want int
	}{
		{modeCorrect, 2},
		{modePlacebo, 2},
		{modeHalfRestore, 2},
		{modeUnregistered, 0},
	}
	for _, item := range cases {
		harness := newReversibilityHarness()
		t.Cleanup(harness.teardown)
		harness.play([]pluginSpec{
			newPluginSpecWithMode("p", item.mode, effectRegisterName, effectOpenStream),
		}, nil)

		if harness.tracked != item.want {
			t.Fatalf("mode=%d 的登记个数 = %d，期望 %d", item.mode, harness.tracked, item.want)
		}
	}
}

// goodSpec 是覆盖全部效果种类的正确插件。
func goodSpec() pluginSpec {
	return newPluginSpec("good",
		effectProvideService, effectRegisterName, effectOpenStream,
		effectStartTimer, effectAcquireHandle, effectBumpShared)
}

func TestPositiveControlsReportNoViolations(t *testing.T) {
	if problems := violationsFor(t, goodSpec()); len(problems) != 0 {
		t.Fatalf("正确插件被误报：%v", problems)
	}
	if problems := violationsFor(t, newPluginSpec("p")); len(problems) != 0 {
		t.Fatalf("无效果的插件被误报：%v", problems)
	}
}

func TestProjectionChangesWhileMountedAndRestoresAfterUnmount(t *testing.T) {
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)
	baseline := harness.project()

	result := harness.play([]pluginSpec{goodSpec()}, nil)

	if reflect.DeepEqual(result.mounted[len(result.mounted)-1], baseline) {
		t.Fatal("投影在挂载期间应确实变化（否则检查器是空转的）")
	}
	if !reflect.DeepEqual(result.unmounted[len(result.unmounted)-1], baseline) {
		t.Fatal("投影在卸载后应确实复原")
	}
}
