package llm_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/llm"
)

func TestRetryingChatRetriesRetryableFailuresWithBackoff(t *testing.T) {
	waits := &waitRecorder{}
	inner := &scriptedResults{name: "p", actions: []func() (llm.Result, error){
		failing(rateLimitedError(0)),
		failing(&llm.Error{Provider: "p", Message: "boom", StatusCode: 503}),
		succeeding("p", "成功"),
	}}
	retrying := llm.NewRetrying(inner,
		llm.WithRetryPolicy(noJitterPolicy()),
		llm.WithRetrySleeper(waits.wait),
	)

	result, err := retrying.Chat(context.Background(), userMessage())

	if err != nil {
		t.Fatalf("重试后应当成功：%v", err)
	}
	if result.Content != "成功" {
		t.Fatalf("结果 = %+v", result)
	}
	if inner.calls != 3 {
		t.Fatalf("尝试次数 = %d，期望 3", inner.calls)
	}
	if got := waits.millis(); !slices.Equal(got, []int64{500, 1000}) {
		t.Fatalf("退避序列 = %v，期望 [500 1000]（指数翻倍）", got)
	}
}

func TestRetryingChatDoesNotRetryUnretryableFailure(t *testing.T) {
	waits := &waitRecorder{}
	inner := &scriptedResults{name: "p", actions: []func() (llm.Result, error){
		failing(&llm.Error{Provider: "p", Message: "unauthorized", StatusCode: 401}),
		succeeding("p", "不该到这里"),
	}}
	retrying := llm.NewRetrying(inner, llm.WithRetrySleeper(waits.wait))

	_, err := retrying.Chat(context.Background(), userMessage())

	var llmError *llm.Error
	if !errors.As(err, &llmError) || llmError.StatusCode != 401 {
		t.Fatalf("错误 = %v，期望原样上抛 401", err)
	}
	if inner.calls != 1 {
		t.Fatal("认证失败换 Key 才有用：拿同一个 Key 重试只是白等")
	}
	if len(waits.waits) != 0 {
		t.Fatalf("不该退避：%v", waits.waits)
	}
}

func TestRetryingChatReturnsLastFailureAfterAttemptsExhausted(t *testing.T) {
	waits := &waitRecorder{}
	inner := &scriptedResults{name: "p", actions: []func() (llm.Result, error){
		failing(&llm.Error{Provider: "p", Message: "boom", StatusCode: 500}),
	}}
	retrying := llm.NewRetrying(inner,
		llm.WithRetryPolicy(attemptsPolicy(3)),
		llm.WithRetrySleeper(waits.wait),
	)

	_, err := retrying.Chat(context.Background(), userMessage())

	var llmError *llm.Error
	if !errors.As(err, &llmError) || llmError.Message != "boom" {
		t.Fatalf("错误 = %v，期望最后一次的原始错误", err)
	}
	if inner.calls != 3 {
		t.Fatalf("尝试次数 = %d，期望 3", inner.calls)
	}
	if len(waits.waits) != 2 {
		t.Fatalf("退避次数 = %d，期望 2（3 次尝试 = 2 次等待）", len(waits.waits))
	}
}

func TestRetryingChatWithNoRetryPolicyTriesOnce(t *testing.T) {
	waits := &waitRecorder{}
	inner := &scriptedResults{name: "p", actions: []func() (llm.Result, error){
		failing(&llm.Error{Provider: "p", Message: "boom", StatusCode: 500}),
	}}
	retrying := llm.NewRetrying(inner,
		llm.WithRetryPolicy(llm.NoRetryPolicy()),
		llm.WithRetrySleeper(waits.wait),
	)

	if _, err := retrying.Chat(context.Background(), userMessage()); err == nil {
		t.Fatal("期望失败")
	}
	if inner.calls != 1 || len(waits.waits) != 0 {
		t.Fatalf("单击次数 = %d、退避 = %v，期望只试一次且不等", inner.calls, waits.waits)
	}
}

func TestRetryingReportsAttemptHonouringRetryAfter(t *testing.T) {
	waits := &waitRecorder{}
	var seen []llm.Attempt
	inner := &scriptedResults{name: "p", actions: []func() (llm.Result, error){
		failing(rateLimitedError(3 * time.Second)),
		succeeding("p", "ok"),
	}}
	retrying := llm.NewRetrying(inner,
		llm.WithRetryPolicy(noJitterPolicy()),
		llm.WithRetrySleeper(waits.wait),
		llm.WithRetryReporter(func(attempt llm.Attempt) { seen = append(seen, attempt) }),
	)

	if _, err := retrying.Chat(context.Background(), userMessage()); err != nil {
		t.Fatalf("重试后应当成功：%v", err)
	}

	if len(seen) != 1 {
		t.Fatalf("通报次数 = %d，期望 1", len(seen))
	}
	attempt := seen[0]
	if attempt.Provider != "p" || attempt.Attempt != 2 || attempt.MaxAttempts != 4 {
		t.Fatalf("通报 = %+v，期望「p 的第 2/4 次」", attempt)
	}
	if attempt.Delay != 3*time.Second {
		t.Fatalf("等待 = %v，期望照服务端说的 3s", attempt.Delay)
	}
	if !attempt.HonoredRetryAfter() {
		t.Fatal("本次等待照服务端说的，HonoredRetryAfter 应为真")
	}
	if want := "p 触发限流，3.0s 后重试（2/4）"; attempt.Summary() != want {
		t.Fatalf("摘要 = %q，期望 %q", attempt.Summary(), want)
	}
}

func TestRetryingForwardsNameAndClose(t *testing.T) {
	inner := &scriptedProvider{name: "my-provider", chatResult: llm.Result{Content: "x"}}
	retrying := llm.NewRetrying(inner, llm.WithRetrySleeper((&waitRecorder{}).wait))

	if got := retrying.Name(); got != "my-provider" {
		t.Fatalf("名字 = %q，期望透传 inner", got)
	}
	if err := retrying.Close(); err != nil {
		t.Fatalf("关闭失败：%v", err)
	}
	if !inner.closed {
		t.Fatal("关闭应透传给被包装的提供商")
	}
}

func TestRetryingStreamRetriesBeforeAnyEvent(t *testing.T) {
	waits := &waitRecorder{}
	inner := &scriptedResults{name: "p", actions: []func() (llm.Result, error){
		failing(&llm.Error{Provider: "p", Message: "boom", StatusCode: 502}),
		succeeding("p", "重试后成功"),
	}}
	retrying := llm.NewRetrying(inner,
		llm.WithRetryPolicy(noJitterPolicy()),
		llm.WithRetrySleeper(waits.wait),
	)

	events := collect(t, retrying.ChatStream(context.Background(), userMessage()))

	if got := joinText(events); got != "重试后成功" {
		t.Fatalf("正文 = %q", got)
	}
	if inner.calls != 2 {
		t.Fatalf("尝试次数 = %d，期望 2", inner.calls)
	}
	if len(waits.waits) != 1 {
		t.Fatalf("退避次数 = %d，期望 1", len(waits.waits))
	}
}

func TestRetryingStreamDoesNotRetryAfterEmitting(t *testing.T) {
	waits := &waitRecorder{}
	inner := &halfStreamProvider{name: "half", err: &llm.Error{
		Provider: "half", Message: "boom", StatusCode: 500,
	}}
	retrying := llm.NewRetrying(inner,
		llm.WithRetryPolicy(noJitterPolicy()),
		llm.WithRetrySleeper(waits.wait),
	)

	var failure error
	for _, err := range retrying.ChatStream(context.Background(), userMessage()) {
		if err != nil {
			failure = err
		}
	}

	if failure == nil {
		t.Fatal("已产出增量后失败应原样上抛")
	}
	if len(waits.waits) != 0 {
		t.Fatal("已经吐出去的增量收不回来：重来会让用户看到重复内容")
	}
	if inner.calls != 1 {
		t.Fatalf("尝试次数 = %d，期望 1", inner.calls)
	}
}

func TestRetryingStreamStopsWhenConsumerStops(t *testing.T) {
	inner := &scriptedResults{name: "p", actions: []func() (llm.Result, error){
		succeeding("p", "a"),
		succeeding("p", "b"),
	}}
	retrying := llm.NewRetrying(inner, llm.WithRetrySleeper((&waitRecorder{}).wait))

	for event, err := range retrying.ChatStream(context.Background(), userMessage()) {
		if err != nil {
			t.Fatalf("流式迭代出错：%v", err)
		}
		if _, ok := event.(llm.TextDelta); ok {
			break
		}
	}

	if inner.calls != 1 {
		t.Fatalf("尝试次数 = %d，消费方停下后不该再试", inner.calls)
	}
}

func TestRetryingAbortsBackoffWhenContextCancelled(t *testing.T) {
	inner := &scriptedResults{name: "p", actions: []func() (llm.Result, error){
		failing(&llm.Error{Provider: "p", Message: "boom", StatusCode: 500}),
	}}
	// 这里用缺省 sleeper：ctx 已取消时必须立刻返回，而不是真的等满 500ms。
	retrying := llm.NewRetrying(inner, llm.WithRetryPolicy(noJitterPolicy()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	_, err := retrying.Chat(ctx, userMessage())

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("错误 = %v，期望 ctx 取消", err)
	}
	if inner.calls != 1 {
		t.Fatalf("尝试次数 = %d，取消后不该再试", inner.calls)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("退避期间取消应立即返回，实际等了 %v", elapsed)
	}
}

func TestDefaultSleeperReturnsAfterDelay(t *testing.T) {
	if err := llm.DefaultSleeper(context.Background(), 0); err != nil {
		t.Fatalf("零延迟应立刻返回：%v", err)
	}
}

// 注入的抖动源必须真的进到退避计算：0.75 倍 → 500ms × 0.75 = 375ms（缺省源是 0.5，即不偏移）。
func TestRetryingAppliesInjectedJitterToBackoff(t *testing.T) {
	waits := &waitRecorder{}
	inner := &scriptedResults{name: "p", actions: []func() (llm.Result, error){
		failing(&llm.Error{Provider: "p", Message: "boom", StatusCode: 500}),
		succeeding("p", "ok"),
	}}
	retrying := llm.NewRetrying(inner,
		llm.WithRetryPolicy(llm.DefaultRetryPolicy()),
		llm.WithRetrySleeper(waits.wait),
		llm.WithRetryJitter(func() float64 { return 0 }),
	)

	if _, err := retrying.Chat(context.Background(), userMessage()); err != nil {
		t.Fatalf("重试后应当成功：%v", err)
	}

	if got := waits.millis(); !slices.Equal(got, []int64{375}) {
		t.Fatalf("退避序列 = %v，期望抖动下界 375ms", got)
	}
}

func TestRetryingStreamAbortsBackoffWhenContextCancelled(t *testing.T) {
	inner := &scriptedResults{name: "p", actions: []func() (llm.Result, error){
		failing(&llm.Error{Provider: "p", Message: "boom", StatusCode: 500}),
	}}
	// 缺省 sleeper：ctx 已取消时必须立刻把取消当结果交出去，而不是真等 500ms。
	retrying := llm.NewRetrying(inner, llm.WithRetryPolicy(noJitterPolicy()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var failure error
	for _, err := range retrying.ChatStream(ctx, userMessage()) {
		if err != nil {
			failure = err
		}
	}

	if !errors.Is(failure, context.Canceled) {
		t.Fatalf("错误 = %v，期望 ctx 取消", failure)
	}
	if inner.calls != 1 {
		t.Fatalf("尝试次数 = %d，取消后不该再试", inner.calls)
	}
}
