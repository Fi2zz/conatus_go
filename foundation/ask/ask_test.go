package ask_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/core"
	"github.com/Fi2zz/conatus_go/foundation/ask"
)

// waitRegistered 等到某个提问已登记（登记是 Ask 的第一步），再投递/取消。
//
// 即便没等到，cancel 相关断言依然成立（那是「取消后再 ask」的另一条路径），
// 所以这里是「尽量确定」而不是「必须」。
func waitRegistered(t *testing.T, instance *ask.CLIAskUser) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if instance.Pending() > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待提问登记超时")
}

// submitWhenReady 反复尝试投递，直到确实有提问在等待。
func submitWhenReady(t *testing.T, instance *ask.CLIAskUser, value string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if instance.Submit(value) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待提问登记超时")
}

// receiveError 收提问结果，超时即判失败。
//
// 带超时是为了「取消失效」这类回归能在 2 秒内失败，而不是把测试挂到整体超时
// （变异测试里每个存活的变异体都会跑一遍，挂 10 分钟会让整轮验证不可用）。
func receiveError(t *testing.T, results <-chan error) error {
	t.Helper()
	select {
	case err := <-results:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("提问未在 2 秒内结束（疑似取消失效而悬挂）")
		return nil
	}
}

func receiveValue(t *testing.T, results <-chan string) string {
	t.Helper()
	select {
	case value := <-results:
		return value
	case <-time.After(2 * time.Second):
		t.Fatal("提问未在 2 秒内结束")
		return ""
	}
}

func TestAskReturnsSubmittedValue(t *testing.T) {
	instance := ask.NewCLIAskUser(ask.WithPromptWriter(io.Discard))
	results := make(chan string, 1)
	go func() {
		value, _ := instance.Ask("名字？")
		results <- value
	}()

	submitWhenReady(t, instance, "Alice")

	if got := receiveValue(t, results); got != "Alice" {
		t.Fatalf("提问结果 = %q，期望 Alice", got)
	}
}

func TestAskWritesPromptToWriter(t *testing.T) {
	buffer := &bytes.Buffer{}
	instance := ask.NewCLIAskUser(ask.WithPromptWriter(buffer))
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = instance.Ask("请选择 A 或 B")
	}()

	submitWhenReady(t, instance, "A")
	<-done

	if !strings.Contains(buffer.String(), "请选择 A 或 B") {
		t.Fatalf("提示未输出：%q", buffer.String())
	}
}

func TestPendingAsksCompleteInFIFOOrder(t *testing.T) {
	instance := ask.NewCLIAskUser(ask.WithPromptWriter(io.Discard))
	first := make(chan string, 1)
	second := make(chan string, 1)

	go func() { value, _ := instance.Ask("a"); first <- value }()
	waitRegistered(t, instance)
	go func() { value, _ := instance.Ask("b"); second <- value }()
	submitWhenReady(t, instance, "1")
	submitWhenReady(t, instance, "2")

	if got := receiveValue(t, first); got != "1" {
		t.Fatalf("首个提问 = %q，期望 1（FIFO）", got)
	}
	if got := receiveValue(t, second); got != "2" {
		t.Fatalf("第二个提问 = %q，期望 2", got)
	}
}

func TestCancelFailsPendingAsk(t *testing.T) {
	instance := ask.NewCLIAskUser(ask.WithPromptWriter(io.Discard))
	results := make(chan error, 1)
	go func() {
		_, err := instance.Ask("x")
		results <- err
	}()
	waitRegistered(t, instance)

	instance.Cancel()

	if err := receiveError(t, results); !errors.Is(err, ask.ErrCancelled) {
		t.Fatalf("取消后等待中的提问应返回 ErrCancelled，得到 %v", err)
	}
}

func TestAskAfterCancelFailsImmediately(t *testing.T) {
	instance := ask.NewCLIAskUser(ask.WithPromptWriter(io.Discard))
	instance.Cancel()

	done := make(chan error, 1)
	go func() {
		_, err := instance.Ask("x")
		done <- err
	}()

	if err := receiveError(t, done); !errors.Is(err, ask.ErrCancelled) {
		t.Fatalf("取消后再提问应立即失败，得到 %v", err)
	}
}

func TestCancelIsIdempotent(t *testing.T) {
	instance := ask.NewCLIAskUser(ask.WithPromptWriter(io.Discard))

	instance.Cancel()
	instance.Cancel()

	if _, err := instance.Ask("x"); !errors.Is(err, ask.ErrCancelled) {
		t.Fatalf("重复取消不该出错，得到 %v", err)
	}
}

func TestSubmitWithoutPendingReportsFalse(t *testing.T) {
	instance := ask.NewCLIAskUser(ask.WithPromptWriter(io.Discard))

	if instance.Submit("没人等") {
		t.Fatal("没有等待中的提问时投递应返回 false")
	}
}

func TestProvideRegistersAskUserService(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	provided, err := ask.Provide(ctx, ask.WithAskUser(ask.NewCLIAskUser(ask.WithPromptWriter(io.Discard))))
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}
	if !ctx.Has(ask.ServiceKey) {
		t.Fatal("服务应已注册")
	}
	from, err := ask.From(ctx)
	if err != nil || from != provided {
		t.Fatalf("From = (%v, %v)，期望同一实例", from, err)
	}
}

func TestProvideDefaultsToCLIAskUser(t *testing.T) {
	ctx := core.NewRoot("root")
	t.Cleanup(ctx.Dispose)

	provided, err := ask.Provide(ctx)
	if err != nil {
		t.Fatalf("provide 失败：%v", err)
	}
	if _, ok := provided.(*ask.CLIAskUser); !ok {
		t.Fatalf("缺省实现 = %T，期望 *ask.CLIAskUser", provided)
	}
}

func TestContextDisposeCancelsPendingAsk(t *testing.T) {
	ctx := core.NewRoot("root")
	instance := ask.NewCLIAskUser(ask.WithPromptWriter(io.Discard))
	if _, err := ask.Provide(ctx, ask.WithAskUser(instance)); err != nil {
		t.Fatalf("provide 失败：%v", err)
	}

	results := make(chan error, 1)
	go func() {
		_, err := instance.Ask("x")
		results <- err
	}()
	waitRegistered(t, instance)

	ctx.Dispose()

	if err := receiveError(t, results); !errors.Is(err, ask.ErrCancelled) {
		t.Fatalf("上下文释放应取消等待中的提问，得到 %v", err)
	}
}
