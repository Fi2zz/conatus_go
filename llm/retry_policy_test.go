package llm_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/llm"
)

func TestDefaultAndNoRetryPolicies(t *testing.T) {
	policy := llm.DefaultRetryPolicy()
	if policy.MaxAttempts != 4 || policy.BaseDelay != 500*time.Millisecond ||
		policy.MaxDelay != 30*time.Second || policy.Jitter != 0.25 {
		t.Fatalf("缺省策略 = %+v", policy)
	}
	if got := llm.NoRetryPolicy().MaxAttempts; got != 1 {
		t.Fatalf("NoRetryPolicy 的尝试次数 = %d，期望 1（只试一次）", got)
	}
}

func TestDelayGrowsExponentiallyFromBase(t *testing.T) {
	policy := llm.DefaultRetryPolicy()

	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 500 * time.Millisecond},
		{2, time.Second},
		{3, 2 * time.Second},
		{4, 4 * time.Second},
	}
	for _, item := range cases {
		if got := policy.DelayFor(item.attempt); got != item.want {
			t.Fatalf("第 %d 次失败后的等待 = %v，期望 %v", item.attempt, got, item.want)
		}
	}
}

func TestDelayIsCappedAtMaxDelay(t *testing.T) {
	policy := llm.DefaultRetryPolicy()

	for _, attempt := range []int{8, 20, 100} {
		if got := policy.DelayFor(attempt); got != policy.MaxDelay {
			t.Fatalf("第 %d 次失败后的等待 = %v，期望被夹到上限 %v", attempt, got, policy.MaxDelay)
		}
	}
}

// TestHugeAttemptDoesNotWrapToZero 钉住 Go 侧刻意收紧的那一点：Dart 在极大 attempt 上
// 会因整型回绕算出「零延迟」（等于疯狂重试），这里必须返回上限。
func TestHugeAttemptDoesNotWrapToZero(t *testing.T) {
	policy := llm.DefaultRetryPolicy()

	if got := policy.DelayFor(1000); got != policy.MaxDelay {
		t.Fatalf("极大 attempt 的等待 = %v，期望上限 %v（不能回绕成 0）", got, policy.MaxDelay)
	}
}

func TestDelayHonoursRetryAfter(t *testing.T) {
	policy := llm.DefaultRetryPolicy()

	if got := policy.DelayFor(1, llm.WithRetryAfter(2*time.Second)); got != 2*time.Second {
		t.Fatalf("服务端 Retry-After 应优先，得到 %v", got)
	}
	if got := policy.DelayFor(1, llm.WithRetryAfter(time.Minute)); got != policy.MaxDelay {
		t.Fatalf("Retry-After 也要封顶，得到 %v，期望 %v", got, policy.MaxDelay)
	}
	// 非正的 Retry-After 视为没有，走指数退避（不叠加服务端意图）。
	if got := policy.DelayFor(1, llm.WithRetryAfter(0)); got != 500*time.Millisecond {
		t.Fatalf("Retry-After 为 0 时应走指数退避，得到 %v", got)
	}
}

func TestDelayAppliesJitterRangeWithInjectedSource(t *testing.T) {
	policy := llm.DefaultRetryPolicy()

	low := policy.DelayFor(1, llm.WithRetryRandom(func() float64 { return 0 }))
	if low != 375*time.Millisecond {
		t.Fatalf("下界抖动 = %v，期望 375ms", low)
	}
	high := policy.DelayFor(1, llm.WithRetryRandom(func() float64 { return 1 }))
	if high != 625*time.Millisecond {
		t.Fatalf("上界抖动 = %v，期望 625ms", high)
	}
}

func TestDelayWithoutJitterIsExact(t *testing.T) {
	policy := llm.DefaultRetryPolicy()
	policy.Jitter = 0

	if got := policy.DelayFor(2); got != time.Second {
		t.Fatalf("关闭抖动后 = %v，期望精确 1s", got)
	}
}

func TestAttemptReportsHonoredRetryAfter(t *testing.T) {
	honored := llm.Attempt{
		Provider: "doubao",
		Attempt:  2,
		Err:      &llm.Error{Provider: "doubao", StatusCode: 429, RetryAfter: 3 * time.Second},
	}
	if !honored.HonoredRetryAfter() {
		t.Fatal("带 Retry-After 的错误应报告为「照服务端说的」")
	}

	plain := llm.Attempt{Provider: "doubao", Attempt: 2, Err: errors.New("连接中断")}
	if plain.HonoredRetryAfter() {
		t.Fatal("普通错误不该报告为照服务端说的")
	}
}
