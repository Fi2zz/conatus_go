// Package ask 把「向用户提问」做成上下文里的服务（对应 Dart 的 ask_user 插件）。
//
// 设计目标与 Dart 版一致：
//
//   - 提问方式由 [AskUser] 接口抽象，命令行只是默认实现；
//   - 提问本身是可逆效应——上下文在等待输入期间被释放，正在进行的提问会被取消
//     （[Provide] 把 Cancel 挂在上下文的释放路径上）；
//   - 同一个 [AskUser] 实例可被多轮对话复用。
//
// 与 Dart 版的形态差异：提问器的等待结果用 `(string, error)` 表达，取消时返回
// [ErrCancelled]（Dart 抛 AskCancelledException），因此判断失败类别用 [errors.Is]。
package ask

import "errors"

// ServiceKey 是提问器在上下文中的服务键。
const ServiceKey = "askUser"

// ErrCancelled 表示提问已被取消：等待中的调用返回它，取消之后的新调用也返回它。
var ErrCancelled = errors.New("提问已取消")

// AskUser 是提问器接口：实现者负责从某个来源取到用户输入。
//
// 实现必须可被多 goroutine 并发使用，且 [AskUser.Cancel] 要幂等。
type AskUser interface {
	// Ask 向用户展示 prompt 并返回其输入；等待期间被 Cancel 时返回 [ErrCancelled]。
	Ask(prompt string) (string, error)
	// Cancel 取消所有正在进行的提问。
	Cancel()
}
