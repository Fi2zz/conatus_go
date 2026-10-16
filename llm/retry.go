package llm

import (
	"context"
	"iter"
	"time"
)

// Sleeper 是睡眠函数。
//
// 可注入是硬要求：退避最长可达 30s，用例不该真的等。
type Sleeper func(ctx context.Context, delay time.Duration) error

// DefaultSleeper 按 ctx 睡眠：到点返回 nil，ctx 先结束就返回它的错误。
//
// 与 Dart 的差别：Dart 用的 `Future.delayed` 打断不了，Go 侧让退避期间也能随时取消
// ——否则一次 30s 退避会把已经放弃的请求按在原地。
func DefaultSleeper(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RetryOption 定制 [Retrying]。
type RetryOption func(*retrySettings)

type retrySettings struct {
	policy   RetryPolicy
	sleep    Sleeper
	jitter   func() float64
	reporter func(Attempt)
}

// WithRetryPolicy 设置退避策略（缺省 [DefaultRetryPolicy]）。
func WithRetryPolicy(policy RetryPolicy) RetryOption {
	return func(s *retrySettings) { s.policy = policy }
}

// WithRetrySleeper 注入睡眠函数：测试用立即返回的实现即可断言退避序列而不真的等。
func WithRetrySleeper(sleep Sleeper) RetryOption {
	return func(s *retrySettings) { s.sleep = sleep }
}

// WithRetryJitter 注入抖动源（返回 [0,1)）；缺省固定 0.5 即不偏移，行为可预期。
func WithRetryJitter(random func() float64) RetryOption {
	return func(s *retrySettings) { s.jitter = random }
}

// WithRetryReporter 通报每次重试（宿主用它把「触发限流，3s 后重试」呈给用户）。
func WithRetryReporter(report func(Attempt)) RetryOption {
	return func(s *retrySettings) { s.reporter = report }
}

// Retrying 是重试装饰器：把 inner 的可重试失败按策略退避重试。
//
// **流式只在尚未产出任何事件时重试**——已经交出去的增量收不回来，静默重来只会让用户
// 看到重复内容（与 [Fallback] 的回退约束同源）。
//
// 重试与回退是正交的两维：典型装配是每个候选各自包一层本装饰器，再由 [Fallback] 串联
// ——先在原提供商上退避重试，仍失败才换下一个。
type Retrying struct {
	inner    Provider
	policy   RetryPolicy
	sleep    Sleeper
	jitter   func() float64
	reporter func(Attempt)
}

// NewRetrying 包装 inner；缺省策略 [DefaultRetryPolicy]、缺省睡眠 [DefaultSleeper]。
func NewRetrying(inner Provider, opts ...RetryOption) *Retrying {
	settings := retrySettings{policy: DefaultRetryPolicy(), sleep: DefaultSleeper}
	for _, opt := range opts {
		opt(&settings)
	}
	return &Retrying{
		inner:    inner,
		policy:   settings.policy,
		sleep:    settings.sleep,
		jitter:   settings.jitter,
		reporter: settings.reporter,
	}
}

// Name 透传 inner 的名字。
func (r *Retrying) Name() string { return r.inner.Name() }

// Close 关闭 inner（它实现了 [Closer] 才真关）。
func (r *Retrying) Close() error { return Close(r.inner) }

// Chat 反复尝试，直到成功、次数用尽或错误不值得重试。
func (r *Retrying) Chat(ctx context.Context, messages []Message, opts ...Option) (Result, error) {
	for attempt := 1; ; attempt++ {
		result, err := r.inner.Chat(ctx, messages, opts...)
		if err == nil {
			return result, nil
		}
		if !r.worthAnother(attempt, err) {
			return Result{}, err
		}
		if waitErr := r.wait(ctx, attempt, err); waitErr != nil {
			return Result{}, waitErr
		}
	}
}

// ChatStream 发送流式请求；重试规则与 [Retrying.Chat] 一致。
func (r *Retrying) ChatStream(
	ctx context.Context,
	messages []Message,
	opts ...Option,
) iter.Seq2[StreamEvent, error] {
	request := streamRequest{messages: messages, options: opts}
	return func(yield func(StreamEvent, error) bool) {
		for attempt := 1; ; attempt++ {
			emitted, stopped, err := pumpStream(ctx, r.inner, request, yield)
			if stopped || err == nil {
				return
			}
			if !r.takeAnother(attempt, emitted, err) {
				yield(nil, err)
				return
			}
			if waitErr := r.wait(ctx, attempt, err); waitErr != nil {
				yield(nil, waitErr)
				return
			}
		}
	}
}

// takeAnother 判断这次失败还能不能再试：**只有没产出过事件**、且错误值得重试时才继续。
//
// 已经交出去的增量收不回来：静默重来只会让用户看到重复内容。
func (r *Retrying) takeAnother(attempt int, emitted bool, err error) bool {
	return !emitted && r.worthAnother(attempt, err)
}

// worthAnother 判断「还有次数，且这个错误重试有意义」。
func (r *Retrying) worthAnother(attempt int, err error) bool {
	return attempt < r.policy.MaxAttempts && IsRetryableError(err)
}

// wait 算出这次的退避时长、通报一次，然后睡够。
func (r *Retrying) wait(ctx context.Context, attempt int, cause error) error {
	delay := r.policy.DelayFor(attempt, WithRetryAfter(RetryAfterOf(cause)), WithRetryRandom(r.jitter))
	if r.reporter != nil {
		r.reporter(Attempt{
			Provider:    r.inner.Name(),
			Attempt:     attempt + 1,
			MaxAttempts: r.policy.MaxAttempts,
			Delay:       delay,
			Err:         cause,
		})
	}
	return r.sleep(ctx, delay)
}
