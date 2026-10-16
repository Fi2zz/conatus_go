package llm

import (
	"context"
	"errors"
	"iter"
	"strings"
)

// fallbackName 是回退链自己的提供商名（出现在汇总错误上）。
const fallbackName = "fallback"

// FallbackEvent 是一次回退：某个候选失败、即将切到下一个时通报一次。
//
// 宿主用它把「主模型不可用，已切到 X」呈给用户——回退是静默发生的，没有这层提示，
// 用户只会看到回答风格突然变了。
type FallbackEvent struct {
	// FromProvider 是失败的提供商名。
	FromProvider string
	// ToProvider 是接下来要试的提供商名。
	ToProvider string
	// Err 是引发回退的原始错误。
	Err error
	// Remaining 是切到 [FallbackEvent.ToProvider] 之后链上还剩几个候选（含它自己）。
	Remaining int
}

// FallbackOption 定制 [Fallback]。
type FallbackOption func(*fallbackSettings)

type fallbackSettings struct {
	reporter func(FallbackEvent)
}

// WithFallbackReporter 通报每次回退。
func WithFallbackReporter(report func(FallbackEvent)) FallbackOption {
	return func(s *fallbackSettings) { s.reporter = report }
}

// Fallback 按顺序尝试多个提供商，直到有一个成功。
//
// 流式回退只在**尚未产出任何事件**时生效：一旦已经交出去增量，中途失败直接上抛
// （已产出的内容收不回来，换一家只会让用户看到重复内容）。全部失败时返回汇总错误，
// 逐个列出每个候选的失败原因。
//
// 重试是另一个维度，见 [Retrying]：典型装配是每个候选各自包一层重试，再由本类型串联。
type Fallback struct {
	providers []Provider
	reporter  func(FallbackEvent)
}

// NewFallback 构造回退链（按参数顺序尝试）。
func NewFallback(providers []Provider, opts ...FallbackOption) *Fallback {
	settings := fallbackSettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	return &Fallback{providers: providers, reporter: settings.reporter}
}

// Name 返回 `fallback`。
func (f *Fallback) Name() string { return fallbackName }

// Close 逐个关掉链上的提供商（实现了 [Closer] 的才真关），错误合并返回。
func (f *Fallback) Close() error {
	var failures []error
	for _, provider := range f.providers {
		if err := Close(provider); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Chat 依次尝试各候选，返回第一个成功的结果。
func (f *Fallback) Chat(ctx context.Context, messages []Message, opts ...Option) (Result, error) {
	failures := make([]string, 0, len(f.providers))
	for index, provider := range f.providers {
		result, err := provider.Chat(ctx, messages, opts...)
		if err == nil {
			return result, nil
		}
		failures = append(failures, provider.Name()+": "+describeFailure(err))
		f.announce(index, provider, err)
	}
	return Result{}, allCandidatesFailed(failures)
}

// ChatStream 依次尝试各候选，把第一个能跑起来的尝试的事件转给消费方。
func (f *Fallback) ChatStream(
	ctx context.Context,
	messages []Message,
	opts ...Option,
) iter.Seq2[StreamEvent, error] {
	request := streamRequest{messages: messages, options: opts}
	return func(yield func(StreamEvent, error) bool) {
		failures := make([]string, 0, len(f.providers))
		for index, provider := range f.providers {
			emitted, stopped, err := pumpStream(ctx, provider, request, yield)
			if stopped || err == nil {
				return
			}
			// 已经吐过增量就收不回来了：上抛，不换提供商。
			if emitted {
				yield(nil, err)
				return
			}
			failures = append(failures, provider.Name()+": "+describeFailure(err))
			f.announce(index, provider, err)
		}
		yield(nil, allCandidatesFailed(failures))
	}
}

// announce 还有下一个候选时通报一次回退。
func (f *Fallback) announce(index int, failed Provider, cause error) {
	if f.reporter == nil || index+1 >= len(f.providers) {
		return
	}
	f.reporter(FallbackEvent{
		FromProvider: failed.Name(),
		ToProvider:   f.providers[index+1].Name(),
		Err:          cause,
		Remaining:    len(f.providers) - index - 1,
	})
}

// describeFailure 取错误文案：LLM 错误用面向用户的那句，其余用 Error()。
func describeFailure(err error) string {
	var llmError *Error
	if errors.As(err, &llmError) {
		return llmError.Message
	}
	return err.Error()
}

// allCandidatesFailed 汇总每个候选的失败。
func allCandidatesFailed(failures []string) error {
	return &Error{
		Provider: fallbackName,
		Message:  "所有提供商均失败：\n" + strings.Join(failures, "\n"),
	}
}
