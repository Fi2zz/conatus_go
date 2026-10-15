package llm_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Fi2zz/conatus_go/llm"
)

func TestInferKindFromStatusCode(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   llm.ErrorKind
	}{
		{"无状态码按网络异常", 0, llm.KindNetwork},
		{"401 认证失败", 401, llm.KindAuth},
		{"403 认证失败", 403, llm.KindAuth},
		{"429 限流", 429, llm.KindRateLimit},
		{"529 限流", 529, llm.KindRateLimit},
		{"500 服务端", 500, llm.KindServer},
		{"503 服务端", 503, llm.KindServer},
		{"400 请求被拒", 400, llm.KindBadRequest},
		{"404 请求被拒", 404, llm.KindBadRequest},
		{"413 请求被拒", 413, llm.KindBadRequest},
	}
	for _, item := range cases {
		if got := llm.InferKind(item.status); got != item.want {
			t.Fatalf("%s：InferKind(%d) = %v，期望 %v", item.name, item.status, got, item.want)
		}
	}
}

func TestErrorKindPrefersExplicitAnnotation(t *testing.T) {
	explicit := &llm.Error{Provider: "doubao", StatusCode: 429, Kind: llm.KindConfig}
	if got := explicit.ErrorKind(); got != llm.KindConfig {
		t.Fatalf("显式标注应优先，得到 %v", got)
	}

	inferred := &llm.Error{Provider: "doubao", StatusCode: 429}
	if got := inferred.ErrorKind(); got != llm.KindRateLimit {
		t.Fatalf("未标注时按状态码推断，得到 %v", got)
	}
	if !inferred.Retryable() {
		t.Fatal("限流错误应可重试")
	}
}

func TestRetryableKinds(t *testing.T) {
	for _, kind := range []llm.ErrorKind{llm.KindRateLimit, llm.KindServer, llm.KindNetwork} {
		if !kind.Retryable() {
			t.Fatalf("%v 应可重试", kind)
		}
	}
	for _, kind := range []llm.ErrorKind{
		llm.KindConfig, llm.KindAuth, llm.KindBadRequest, llm.KindUnset,
	} {
		if kind.Retryable() {
			t.Fatalf("%v 不该重试", kind)
		}
	}
}

func TestKindLabelsAndNames(t *testing.T) {
	labels := map[llm.ErrorKind]string{
		llm.KindConfig:     "配置不完整",
		llm.KindAuth:       "认证失败",
		llm.KindRateLimit:  "触发限流",
		llm.KindServer:     "服务端错误",
		llm.KindNetwork:    "网络异常",
		llm.KindBadRequest: "请求被拒",
	}
	for kind, want := range labels {
		if got := kind.Label(); got != want {
			t.Fatalf("%v.Label() = %q，期望 %q", kind, got, want)
		}
	}
	if got := llm.KindRateLimit.String(); got != "rateLimit" {
		t.Fatalf("KindRateLimit.String() = %q，期望 rateLimit", got)
	}
	if got := llm.KindUnset.String(); got != "unset" {
		t.Fatalf("KindUnset.String() = %q，期望 unset", got)
	}
}

func TestErrorTextIsStable(t *testing.T) {
	err := &llm.Error{Provider: "doubao", StatusCode: 429, Message: "限流了"}

	if got := err.Error(); got != "LlmException(doubao, 429): 限流了" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestIsRetryableError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"限流错误可重试", &llm.Error{Provider: "p", StatusCode: 429}, true},
		{"服务端错误可重试", &llm.Error{Provider: "p", StatusCode: 503}, true},
		{"认证错误不可重试", &llm.Error{Provider: "p", StatusCode: 401}, false},
		{"请求被拒不可重试", &llm.Error{Provider: "p", StatusCode: 400}, false},
		{"超时按传输失败算可重试", context.DeadlineExceeded, true},
		{"包装后的超时也能识别", fmt.Errorf("请求失败：%w", context.DeadlineExceeded), true},
		{"普通错误（编程错误）不可重试", errors.New("断言失败"), false},
		{"nil 不可重试", nil, false},
	}
	for _, item := range cases {
		if got := llm.IsRetryableError(item.err); got != item.want {
			t.Fatalf("%s：IsRetryableError = %v，期望 %v", item.name, got, item.want)
		}
	}
}

func TestRetryAfterOf(t *testing.T) {
	withRetryAfter := &llm.Error{Provider: "p", StatusCode: 429, RetryAfter: 3 * time.Second}

	if got := llm.RetryAfterOf(withRetryAfter); got != 3*time.Second {
		t.Fatalf("Retry-After = %v，期望 3s", got)
	}
	if got := llm.RetryAfterOf(errors.New("x")); got != 0 {
		t.Fatalf("非 LLM 错误的 Retry-After = %v，期望 0", got)
	}
	if got := llm.RetryAfterOf(nil); got != 0 {
		t.Fatalf("nil 的 Retry-After = %v，期望 0", got)
	}
}
