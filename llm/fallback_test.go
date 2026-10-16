package llm_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Fi2zz/conatus_go/llm"
)

func TestFallbackUsesFirstCandidate(t *testing.T) {
	primary := &scriptedResults{name: "主", actions: []func() (llm.Result, error){
		succeeding("主", "主答"),
	}}
	backup := &scriptedResults{name: "备", actions: []func() (llm.Result, error){
		succeeding("备", "备答"),
	}}
	chain := llm.NewFallback([]llm.Provider{primary, backup})

	result, err := chain.Chat(context.Background(), userMessage())

	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	if result.Content != "主答" {
		t.Fatalf("结果 = %+v", result)
	}
	if backup.calls != 0 {
		t.Fatal("首选成功时不该调用备选（白花配额）")
	}
	if got := chain.Name(); got != "fallback" {
		t.Fatalf("回退链名字 = %q，期望 fallback", got)
	}
}

func TestFallbackSwitchesToNextCandidate(t *testing.T) {
	primary := &scriptedResults{name: "主", actions: []func() (llm.Result, error){
		failing(&llm.Error{Provider: "主", Message: "a", StatusCode: 500}),
	}}
	backup := &scriptedResults{name: "备", actions: []func() (llm.Result, error){
		succeeding("备", "备答"),
	}}
	chain := llm.NewFallback([]llm.Provider{primary, backup})

	result, err := chain.Chat(context.Background(), userMessage())

	if err != nil {
		t.Fatalf("回退后应当成功：%v", err)
	}
	if result.Content != "备答" || result.Provider != "备" {
		t.Fatalf("结果 = %+v，期望切到备选", result)
	}
	if primary.calls != 1 || backup.calls != 1 {
		t.Fatalf("调用次数 = (%d, %d)，期望各一次", primary.calls, backup.calls)
	}
}

func TestFallbackAggregatesEveryCandidateFailure(t *testing.T) {
	chain := llm.NewFallback([]llm.Provider{
		&scriptedResults{name: "主", actions: []func() (llm.Result, error){
			failing(&llm.Error{Provider: "主", Message: "a", StatusCode: 500}),
		}},
		&scriptedResults{name: "备", actions: []func() (llm.Result, error){
			failing(&llm.Error{Provider: "备", Message: "b", StatusCode: 429}),
		}},
	})

	_, err := chain.Chat(context.Background(), userMessage())

	var llmError *llm.Error
	if !errors.As(err, &llmError) {
		t.Fatalf("错误 = %v，期望 *llm.Error", err)
	}
	if llmError.Provider != "fallback" {
		t.Fatalf("汇总错误挂在 %q 上，期望 fallback", llmError.Provider)
	}
	if !strings.Contains(llmError.Message, "主: a") || !strings.Contains(llmError.Message, "备: b") {
		t.Fatalf("汇总文案 = %q，期望逐个列出每个候选的原因", llmError.Message)
	}
}

func TestFallbackWithoutCandidatesFailsWithoutPanic(t *testing.T) {
	chain := llm.NewFallback(nil)

	if _, err := chain.Chat(context.Background(), userMessage()); err == nil {
		t.Fatal("空链应返回汇总错误，而不是 panic")
	}
}

func TestFallbackReportsSwitchWithRemaining(t *testing.T) {
	var seen []llm.FallbackEvent
	first := &scriptedResults{name: "一", actions: []func() (llm.Result, error){
		failing(errors.New("一坏")),
	}}
	second := &scriptedResults{name: "二", actions: []func() (llm.Result, error){
		failing(errors.New("二坏")),
	}}
	third := &scriptedResults{name: "三", actions: []func() (llm.Result, error){
		succeeding("三", "三答"),
	}}
	chain := llm.NewFallback([]llm.Provider{first, second, third},
		llm.WithFallbackReporter(func(event llm.FallbackEvent) { seen = append(seen, event) }),
	)

	if _, err := chain.Chat(context.Background(), userMessage()); err != nil {
		t.Fatalf("请求失败：%v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("通报次数 = %d，期望 2（最后一次失败后面没有候选了）", len(seen))
	}
	if seen[0].FromProvider != "一" || seen[0].ToProvider != "二" || seen[0].Remaining != 2 {
		t.Fatalf("首次回退 = %+v", seen[0])
	}
	if seen[1].FromProvider != "二" || seen[1].ToProvider != "三" || seen[1].Remaining != 1 {
		t.Fatalf("第二次回退 = %+v", seen[1])
	}
	if !strings.Contains(seen[0].Err.Error(), "一坏") {
		t.Fatalf("回退事件应带上原始错误：%v", seen[0].Err)
	}
}

func TestFallbackStreamUsesFirstWorkingCandidate(t *testing.T) {
	primary := &scriptedResults{name: "主", actions: []func() (llm.Result, error){
		failing(&llm.Error{Provider: "主", Message: "boom", StatusCode: 500}),
	}}
	backup := &scriptedResults{name: "备", actions: []func() (llm.Result, error){
		succeeding("备", "ok"),
	}}
	chain := llm.NewFallback([]llm.Provider{primary, backup})

	events := collect(t, chain.ChatStream(context.Background(), userMessage()))

	if got := joinText(events); got != "ok" {
		t.Fatalf("正文 = %q，期望来自备选", got)
	}
	if primary.calls != 1 || backup.calls != 1 {
		t.Fatalf("调用次数 = (%d, %d)，期望各一次", primary.calls, backup.calls)
	}
}

func TestFallbackStreamDoesNotFallBackAfterEmitting(t *testing.T) {
	primary := &halfStreamProvider{name: "half", err: &llm.Error{
		Provider: "half", Message: "boom", StatusCode: 500,
	}}
	backup := &scriptedResults{name: "备", actions: []func() (llm.Result, error){
		succeeding("备", "不该到这里"),
	}}
	chain := llm.NewFallback([]llm.Provider{primary, backup})

	var failure error
	for _, err := range chain.ChatStream(context.Background(), userMessage()) {
		if err != nil {
			failure = err
		}
	}

	if failure == nil {
		t.Fatal("已产出增量后失败应上抛")
	}
	if backup.calls != 0 {
		t.Fatal("换一家会让用户看到重复内容：已产出的增量收不回来")
	}
}

func TestFallbackStreamStopsWhenConsumerStops(t *testing.T) {
	first := &scriptedResults{name: "一", actions: []func() (llm.Result, error){
		succeeding("一", "a"),
	}}
	second := &scriptedResults{name: "二", actions: []func() (llm.Result, error){
		succeeding("二", "b"),
	}}
	chain := llm.NewFallback([]llm.Provider{first, second})

	for event, err := range chain.ChatStream(context.Background(), userMessage()) {
		if err != nil {
			t.Fatalf("流式迭代出错：%v", err)
		}
		if _, ok := event.(llm.TextDelta); ok {
			break
		}
	}

	if first.calls != 1 || second.calls != 0 {
		t.Fatalf("调用次数 = (%d, %d)，消费方停下后不该再试下一个", first.calls, second.calls)
	}
}

func TestFallbackStreamForwardsTools(t *testing.T) {
	transport := &fakeTransport{body: sseBody(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"get_time","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		"[DONE]",
	)}
	chain := llm.NewFallback([]llm.Provider{newTestProvider(t, transport)})

	events := collect(t, chain.ChatStream(context.Background(), userMessage(), llm.WithTools(testTools())))

	if _, found := onlyRequest(t, transport).bodyObject(t)["tools"]; !found {
		t.Fatal("回退链必须把 tools 透传给提供商")
	}
	if calls := doneOf(t, events).ToolCalls; len(calls) != 1 || calls[0].Name != "get_time" {
		t.Fatalf("工具调用 = %+v", calls)
	}
}

func TestFallbackCloseClosesEveryCandidate(t *testing.T) {
	first := &scriptedProvider{name: "一"}
	second := &scriptedProvider{name: "二"}
	chain := llm.NewFallback([]llm.Provider{first, second})

	if err := chain.Close(); err != nil {
		t.Fatalf("关闭失败：%v", err)
	}
	if !first.closed || !second.closed {
		t.Fatal("链上每个候选都要关掉，不能只关第一个")
	}
}

// 关闭错误不许被吞：汇总错误要带上是谁关不掉。
func TestFallbackCloseReportsFailures(t *testing.T) {
	chain := llm.NewFallback([]llm.Provider{
		&closeFailingProvider{name: "一"},
		&scriptedProvider{name: "二"},
	})

	err := chain.Close()

	if err == nil || !strings.Contains(err.Error(), "一 关不掉") {
		t.Fatalf("错误 = %v，期望带上关不掉的候选名", err)
	}
}

func TestFallbackStreamAggregatesWhenEveryCandidateFails(t *testing.T) {
	chain := llm.NewFallback([]llm.Provider{
		&scriptedResults{name: "主", actions: []func() (llm.Result, error){
			failing(&llm.Error{Provider: "主", Message: "a", StatusCode: 500}),
		}},
		&scriptedResults{name: "备", actions: []func() (llm.Result, error){
			failing(&llm.Error{Provider: "备", Message: "b", StatusCode: 429}),
		}},
	})

	var failure error
	for _, err := range chain.ChatStream(context.Background(), userMessage()) {
		if err != nil {
			failure = err
		}
	}

	var llmError *llm.Error
	if !errors.As(failure, &llmError) {
		t.Fatalf("错误 = %v，期望汇总的 *llm.Error", failure)
	}
	if !strings.Contains(llmError.Message, "主: a") || !strings.Contains(llmError.Message, "备: b") {
		t.Fatalf("汇总文案 = %q，期望逐个列出每个候选的原因", llmError.Message)
	}
}

// 重试与回退串联：先在主提供商退避重试，仍失败才切到备选（对应 Dart 的同名用例）。
func TestRetryingInsideFallbackReportsRetryThenFallback(t *testing.T) {
	primaryWaits := &waitRecorder{}
	backupWaits := &waitRecorder{}
	var events []string
	primary := &scriptedResults{name: "主", actions: []func() (llm.Result, error){
		failing(&llm.Error{Provider: "主", Message: "boom", StatusCode: 500}),
	}}
	backup := &scriptedResults{name: "备", actions: []func() (llm.Result, error){
		succeeding("备", "备选答"),
	}}
	chain := llm.NewFallback(
		[]llm.Provider{
			llm.NewRetrying(primary,
				llm.WithRetryPolicy(attemptsPolicy(2)),
				llm.WithRetrySleeper(primaryWaits.wait),
				llm.WithRetryReporter(func(attempt llm.Attempt) {
					events = append(events, "retry:"+attempt.Provider)
				}),
			),
			llm.NewRetrying(backup,
				llm.WithRetryPolicy(attemptsPolicy(2)),
				llm.WithRetrySleeper(backupWaits.wait),
			),
		},
		llm.WithFallbackReporter(func(event llm.FallbackEvent) {
			events = append(events, "fallback:"+event.FromProvider+"->"+event.ToProvider)
		}),
	)

	result, err := chain.Chat(context.Background(), userMessage())

	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	if result.Content != "备选答" {
		t.Fatalf("结果 = %+v", result)
	}
	if primary.calls != 2 {
		t.Fatalf("主提供商尝试次数 = %d，期望先自己重试一次", primary.calls)
	}
	if len(primaryWaits.waits) != 1 || len(backupWaits.waits) != 0 {
		t.Fatalf("退避次数 = (%d, %d)，备选一次成功不该等", len(primaryWaits.waits), len(backupWaits.waits))
	}
	if want := []string{"retry:主", "fallback:主->备"}; !slices.Equal(events, want) {
		t.Fatalf("事件序列 = %v，期望 %v", events, want)
	}
}
