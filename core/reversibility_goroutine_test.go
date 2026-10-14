package core_test

import (
	"runtime"
	"testing"
	"time"
)

// Go 特有的资源探针：Dart 靠 GC 与事件循环保证「协程」不泄漏，Go 必须显式证明
// 卸载后没有 goroutine 残留、真实定时器不再触发。这是移植到 Go 才出现的新风险面，
// 因此 Dart 版没有对应文件。
func TestMountUnmountDoesNotLeakGoroutines(t *testing.T) {
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)
	specs := []pluginSpec{
		newPluginSpec("a", effectStartTimer, effectOpenStream),
		newPluginSpec("b", effectOpenStream, effectAcquireHandle, effectStartTimer),
	}

	// 预热一轮，让后台 goroutine 全部退出后再取基线。
	if problems := harness.runLifo(specs); len(problems) != 0 {
		t.Fatalf("预热轮未还原：%v", problems)
	}
	baseline := settleGoroutines()

	for round := 0; round < 50; round++ {
		if problems := harness.runLifo(specs); len(problems) != 0 {
			t.Fatalf("第 %d 轮未还原：%v", round, problems)
		}
	}

	if after := waitForGoroutinesAtMost(t, baseline); after > baseline {
		t.Fatalf("挂载/卸载后 goroutine 数从 %d 涨到 %d（疑似泄漏）", baseline, after)
	}
}

func TestTimersStopTickingAfterUnmount(t *testing.T) {
	harness := newReversibilityHarness()
	t.Cleanup(harness.teardown)

	harness.play([]pluginSpec{newPluginSpec("t", effectStartTimer)}, nil)

	if !harness.world.allReleased() {
		t.Fatal("卸载后定时器应已停止")
	}
	before := harness.world.totalTicks()
	time.Sleep(40 * time.Millisecond)
	if after := harness.world.totalTicks(); after != before {
		t.Fatalf("卸载后定时器仍在触发：tick %d → %d（逆没真正停掉 OS 定时器）", before, after)
	}
}

// settleGoroutines 等到 goroutine 数连续若干个采样窗口不再下降，返回该稳定值。
func settleGoroutines() int {
	previous := runtime.NumGoroutine()
	stable := 0
	for stable < 3 {
		time.Sleep(5 * time.Millisecond)
		current := runtime.NumGoroutine()
		if current >= previous {
			stable++
		} else {
			stable = 0
		}
		previous = current
	}
	return previous
}

// waitForGoroutinesAtMost 轮询等待 goroutine 数降到 limit 以内（最多 2 秒），
// 避免用 sleep 猜时机。
func waitForGoroutinesAtMost(t *testing.T, limit int) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	count := runtime.NumGoroutine()
	for count > limit && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		count = runtime.NumGoroutine()
	}
	return count
}
