package agent

import (
	"context"
	"errors"

	"github.com/Fi2zz/conatus_go/llm"
)

// ErrCancelled 表示这一轮被取消：ctx 结束后由 [Loop.Run] 返回。
//
// 与 Dart 的差异：Dart 的 `AgentCancel` 是「竞速停止等待」——Dart 杀不掉在途 Future，
// 只能谁先完成听谁的，底层工作可能还在后台跑完、结果被丢弃。Go 的 ctx 会**真正**取消
// 在途工作（HTTP 请求与工具执行体都拿得到它），所以这里不再另造取消信号，只看 ctx。
//
// 代价也写清楚，免得日后误判：无视 ctx 的第三方提供方或工具会让这一轮多等一会——它的
// 结果注定被丢掉，但循环得等它返回才收手。
var ErrCancelled = errors.New("轮次已取消")

// modelCall 跑一次模型调用，把「ctx 已结束」翻译成 [ErrCancelled]。
//
// 传输层错误的形状取决于具体提供方（可能是 url.Error、context.Canceled 或自定义包装），
// 所以一律以 ctx 的状态为准，不去认错误类型。
func modelCall(ctx context.Context, call func() (llm.Result, error)) (llm.Result, error) {
	result, err := call()
	if err == nil {
		return result, nil
	}
	if ctx.Err() != nil {
		return llm.Result{}, ErrCancelled
	}
	return llm.Result{}, err
}
