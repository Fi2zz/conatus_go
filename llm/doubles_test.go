package llm_test

import (
	"context"
	"errors"
	"iter"
	"time"

	"github.com/Fi2zz/conatus_go/llm"
)

// 本文件是重试 / 回退两条装饰器路径共用的测试替身。

// waitRecorder 记录每次退避时长；它自己立即返回，用例不必真的等。
type waitRecorder struct {
	waits []time.Duration
}

// wait 记录一次退避时长并立即返回（作为 [llm.Sleeper] 注入）。
func (r *waitRecorder) wait(_ context.Context, delay time.Duration) error {
	r.waits = append(r.waits, delay)
	return nil
}

// millis 把记录的退避时长换成毫秒序列（便于整体比较）。
func (r *waitRecorder) millis() []int64 {
	values := make([]int64, 0, len(r.waits))
	for _, wait := range r.waits {
		values = append(values, wait.Milliseconds())
	}
	return values
}

// scriptedResults 按脚本逐次给出 Chat / ChatStream 的结果；脚本用尽后停在最后一个动作上。
type scriptedResults struct {
	name    string
	actions []func() (llm.Result, error)
	calls   int
}

func (p *scriptedResults) Name() string { return p.name }

func (p *scriptedResults) Chat(context.Context, []llm.Message, ...llm.Option) (llm.Result, error) {
	return p.next()()
}

func (p *scriptedResults) ChatStream(
	context.Context, []llm.Message, ...llm.Option,
) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		result, err := p.next()()
		if err != nil {
			yield(nil, err)
			return
		}
		yield(llm.TextDelta{Text: result.Content}, nil)
	}
}

// next 取出本次要跑的动作并计数。
func (p *scriptedResults) next() func() (llm.Result, error) {
	index := p.calls
	if index >= len(p.actions) {
		index = len(p.actions) - 1
	}
	p.calls++
	return p.actions[index]
}

// halfStreamProvider 先吐一个增量再失败：用来钉「已产出就不重试 / 不回退」。
type halfStreamProvider struct {
	name  string
	err   error
	calls int
}

func (p *halfStreamProvider) Name() string { return p.name }

func (p *halfStreamProvider) Chat(context.Context, []llm.Message, ...llm.Option) (llm.Result, error) {
	return llm.Result{}, p.err
}

func (p *halfStreamProvider) ChatStream(
	context.Context, []llm.Message, ...llm.Option,
) iter.Seq2[llm.StreamEvent, error] {
	p.calls++
	return func(yield func(llm.StreamEvent, error) bool) {
		if !yield(llm.TextDelta{Text: "半句"}, nil) {
			return
		}
		yield(nil, p.err)
	}
}

// failing 是一个必定失败的动作。
func failing(err error) func() (llm.Result, error) {
	return func() (llm.Result, error) { return llm.Result{}, err }
}

// succeeding 是一个成功的动作（带上提供商名，便于断言回退切到了哪家）。
func succeeding(provider, content string) func() (llm.Result, error) {
	return func() (llm.Result, error) {
		return llm.Result{Content: content, Provider: provider, Model: "m"}, nil
	}
}

// rateLimitedError 是带 Retry-After 的限流错误。
func rateLimitedError(after time.Duration) *llm.Error {
	return &llm.Error{Provider: "p", Message: "slow down", StatusCode: 429, RetryAfter: after}
}

// noJitterPolicy 关掉抖动，退避序列才可精确断言（注入抖动源等于把随机性交回用例）。
func noJitterPolicy() llm.RetryPolicy {
	policy := llm.DefaultRetryPolicy()
	policy.Jitter = 0
	return policy
}

// attemptsPolicy 指定尝试次数并关掉抖动。
func attemptsPolicy(maxAttempts int) llm.RetryPolicy {
	policy := noJitterPolicy()
	policy.MaxAttempts = maxAttempts
	return policy
}

// closeFailingProvider 的 Close 会失败：用来钉「关闭错误不被吞掉」。
type closeFailingProvider struct {
	name string
}

func (p *closeFailingProvider) Name() string { return p.name }

func (p *closeFailingProvider) Chat(context.Context, []llm.Message, ...llm.Option) (llm.Result, error) {
	return llm.Result{}, nil
}

func (p *closeFailingProvider) ChatStream(
	context.Context, []llm.Message, ...llm.Option,
) iter.Seq2[llm.StreamEvent, error] {
	return func(func(llm.StreamEvent, error) bool) {}
}

func (p *closeFailingProvider) Close() error { return errors.New(p.name + " 关不掉") }
