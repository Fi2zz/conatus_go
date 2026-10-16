package llm

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
)

// 缺省重试参数。
const (
	// DefaultMaxAttempts 是缺省总尝试次数（含首次请求）。
	DefaultMaxAttempts = 4
	// DefaultBaseDelay 是缺省首次退避时长。
	DefaultBaseDelay = 500 * time.Millisecond
	// DefaultMaxDelay 是缺省单次退避硬上限。
	DefaultMaxDelay = 30 * time.Second
	// DefaultJitter 是缺省抖动比例（±25%）。
	DefaultJitter = 0.25
)

// RetryPolicy 是退避重试策略：次数、间隔上限与抖动。
//
// 零值不是合法策略（MaxAttempts 0 表示一次都不试），请用 [DefaultRetryPolicy] 或
// [NoRetryPolicy]。
type RetryPolicy struct {
	// MaxAttempts 是总尝试次数（**含**首次请求），1 表示不重试。
	MaxAttempts int
	// BaseDelay 是首次退避时长；此后按 2 的幂翻倍。
	BaseDelay time.Duration
	// MaxDelay 是单次退避的硬上限。
	MaxDelay time.Duration
	// Jitter 是抖动比例（0.25 = ±25%）。
	//
	// 并发客户端不加抖动会踩同一个节奏重试，形成惊群；服务端给了 `Retry-After` 时
	// 按服务端说的等，不再叠加抖动。
	Jitter float64
}

// DefaultRetryPolicy 返回缺省策略（4 次尝试 / 500ms 起 / 30s 上限 / ±25% 抖动）。
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: DefaultMaxAttempts,
		BaseDelay:   DefaultBaseDelay,
		MaxDelay:    DefaultMaxDelay,
		Jitter:      DefaultJitter,
	}
}

// NoRetryPolicy 只试一次（等价于关闭重试）。
func NoRetryPolicy() RetryPolicy {
	policy := DefaultRetryPolicy()
	policy.MaxAttempts = 1
	return policy
}

// DelayOption 定制 [RetryPolicy.DelayFor]。
type DelayOption func(*delaySettings)

type delaySettings struct {
	retryAfter time.Duration
	random     func() float64
}

// WithRetryAfter 传入服务端 `Retry-After`：为正时按服务端说的等，且不叠加抖动。
func WithRetryAfter(delay time.Duration) DelayOption {
	return func(s *delaySettings) { s.retryAfter = delay }
}

// WithRetryRandom 注入抖动源（返回 [0,1)）；缺省固定取 0.5，即不偏移、行为可预期。
func WithRetryRandom(random func() float64) DelayOption {
	return func(s *delaySettings) { s.random = random }
}

// DelayFor 返回第 attempt 次尝试失败后（1 起）到下一次尝试之间的等待时长。
func (p RetryPolicy) DelayFor(attempt int, opts ...DelayOption) time.Duration {
	settings := delaySettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	if settings.retryAfter > 0 {
		return p.cap(settings.retryAfter)
	}

	shift := 0
	if attempt > 1 {
		shift = attempt - 1
	}
	if shift > saturationShift(p) {
		// 翻倍早已超过上限：直接短路，避免整型回绕。
		//
		// 这是 Go 侧相对 Dart 的一处刻意收紧：Dart 在 attempt 极大时 1 << shift 会回绕成
		// 负数，再经 _cap 变成「零延迟」（等于疯狂重试）；这里返回上限。
		// 可达的 attempt 范围内（shift ≤ ~60）两者行为一致，都是上限。
		return p.cap(p.MaxDelay)
	}
	return p.cap(p.applyJitter(p.BaseDelay*(1<<shift), settings.random))
}

// applyJitter 按 [RetryPolicy.Jitter] 在 ±jitter 比例内偏移；jitter ≤ 0 时不偏移。
func (p RetryPolicy) applyJitter(delay time.Duration, random func() float64) time.Duration {
	if p.Jitter <= 0 {
		return delay
	}
	unit := 0.5
	if random != nil {
		unit = random()
	}
	factor := 1 - p.Jitter + 2*p.Jitter*unit
	return time.Duration(math.Round(float64(delay) * factor))
}

// cap 把退避夹到 [0, MaxDelay]。
func (p RetryPolicy) cap(delay time.Duration) time.Duration {
	if delay > p.MaxDelay {
		return p.MaxDelay
	}
	if delay < 0 {
		return 0
	}
	return delay
}

// saturationShift 返回「再翻倍也不会低于上限」的最小位移量。
func saturationShift(p RetryPolicy) int {
	if p.BaseDelay <= 0 {
		return 0
	}
	shift := 0
	for scaled := p.BaseDelay; scaled < p.MaxDelay && shift < 62; shift++ {
		scaled *= 2
	}
	return shift
}

// Attempt 是一次重试的通知；宿主用它把「限流，2s 后重试」呈给用户。
type Attempt struct {
	// Provider 是提供商名。
	Provider string
	// Attempt 是即将进行的第几次尝试（1 起）。
	Attempt int
	// MaxAttempts 是总尝试次数。
	MaxAttempts int
	// Delay 是这次等待了多久。
	Delay time.Duration
	// Err 是引起重试的原始错误。
	Err error
}

// HonoredRetryAfter 返回本次等待是否照服务端说的（而非叠加抖动算出来的）。
func (a Attempt) HonoredRetryAfter() bool { return RetryAfterOf(a.Err) > 0 }

// Summary 返回一行中文摘要（如「doubao 触发限流，3.0s 后重试（2/4）」），供屏上直接显示。
func (a Attempt) Summary() string {
	var llmError *Error
	reason := KindNetwork.Label()
	if errors.As(a.Err, &llmError) {
		reason = llmError.ErrorKind().Label()
	}
	return fmt.Sprintf(
		"%s %s，%ss 后重试（%d/%d）",
		a.Provider, reason, secondsText(a.Delay), a.Attempt, a.MaxAttempts,
	)
}

// secondsText 按一位小数给出秒数（3.0 / 0.5），与 Dart 的 toStringAsFixed(1) 对齐。
func secondsText(delay time.Duration) string {
	return strconv.FormatFloat(delay.Seconds(), 'f', 1, 64)
}
