package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

// ErrorKind 是调用失败的类别。
//
// 只管重试，不管回退：401 在重试维度是死路，但在回退维度完全该走（换一个有 Key 的
// 提供商）。重试策略（隔多久、几次）见 retry.go；回退见 fallback.go。
type ErrorKind int

const (
	// KindUnset 表示未标注（由 HTTP 状态码推断，见 [Error.ErrorKind]）。
	KindUnset ErrorKind = iota
	// KindConfig 配置问题：缺 API Key、端点不合法。重试无意义。
	KindConfig
	// KindAuth 认证 / 授权失败（401 / 403）。重试无意义。
	KindAuth
	// KindRateLimit 限流（429、529）。**该**退避重试。
	KindRateLimit
	// KindServer 服务端错误（5xx）。该重试。
	KindServer
	// KindNetwork 传输层失败：超时、连接被拒、连接中断。该重试。
	KindNetwork
	// KindBadRequest 请求本身被拒（400 / 404 / 413 …）。重试只会再被拒一次。
	KindBadRequest
)

// InferKind 由 HTTP 状态码推断类别；statusCode 为 0（无状态码）按 [KindNetwork] 处理。
func InferKind(statusCode int) ErrorKind {
	switch {
	case statusCode == 0:
		return KindNetwork
	case statusCode == 401 || statusCode == 403:
		return KindAuth
	case statusCode == 429 || statusCode == 529:
		return KindRateLimit
	case statusCode >= 500:
		return KindServer
	default:
		return KindBadRequest
	}
}

// Retryable 返回重试是否可能得到不同结果。
func (k ErrorKind) Retryable() bool {
	return k == KindRateLimit || k == KindServer || k == KindNetwork
}

// Label 返回面向用户的中文原因短语。
func (k ErrorKind) Label() string {
	switch k {
	case KindConfig:
		return "配置不完整"
	case KindAuth:
		return "认证失败"
	case KindRateLimit:
		return "触发限流"
	case KindServer:
		return "服务端错误"
	case KindNetwork:
		return "网络异常"
	case KindBadRequest:
		return "请求被拒"
	default:
		return "未知错误"
	}
}

func (k ErrorKind) String() string {
	switch k {
	case KindConfig:
		return "config"
	case KindAuth:
		return "auth"
	case KindRateLimit:
		return "rateLimit"
	case KindServer:
		return "server"
	case KindNetwork:
		return "network"
	case KindBadRequest:
		return "badRequest"
	default:
		return "unset"
	}
}

// Error 是提供商调用失败。
//
// Kind 由 wire 层在失败时显式标注——传输层失败（超时 / 连接错误）没有状态码，只能
// 显式标；未标注（[KindUnset]）时 [Error.ErrorKind] 按 StatusCode 推断。
type Error struct {
	Provider string
	Message  string
	// StatusCode 是 HTTP 状态码；0 表示没有状态码（传输层失败）。
	StatusCode int
	// Kind 是显式标注的失败类别；[KindUnset] 表示按 StatusCode 推断。
	Kind ErrorKind
	// RetryAfter 是服务端 `Retry-After` 解析出的等待时长；0 表示没有。
	RetryAfter time.Duration
}

// ErrorKind 返回失败类别：显式标注优先，否则按状态码推断。
func (e *Error) ErrorKind() ErrorKind {
	if e.Kind == KindUnset {
		return InferKind(e.StatusCode)
	}
	return e.Kind
}

// Retryable 返回重试是否可能得到不同结果。
func (e *Error) Retryable() bool { return e.ErrorKind().Retryable() }

func (e *Error) Error() string {
	return fmt.Sprintf("LlmException(%s, %d): %s", e.Provider, e.StatusCode, e.Message)
}

// IsRetryableError 判断该错误是否值得重试。
//
// [Error] 按自身类别判定；传输层失败（超时 / 连接被拒 / 连接中断）一律算可重试；
// 其余类型（断言失败、参数类型错误……）视为编程错误，重试只会重复失败。
func IsRetryableError(err error) bool {
	var llmError *Error
	if errors.As(err, &llmError) {
		return llmError.Retryable()
	}
	return isTransportFailure(err)
}

// RetryAfterOf 取出服务端给的 `Retry-After`（已解析成时长）；没有则返回 0。
func RetryAfterOf(err error) time.Duration {
	var llmError *Error
	if errors.As(err, &llmError) {
		return llmError.RetryAfter
	}
	return 0
}

// isTransportFailure 判断是否是 Go 侧的传输层失败
// （对应 Dart 的 TimeoutException / SocketException / HandshakeException）。
func isTransportFailure(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) {
		return true
	}
	var netError net.Error
	if errors.As(err, &netError) {
		return true
	}
	return false
}
