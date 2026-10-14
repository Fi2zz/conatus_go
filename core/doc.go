// Package core 是 conatus 时空可组合性范式的 Go 实现：可逆效应
// （[EffectScope]）、反应式共效应（[Reactor]）与统一上下文（[Context]），
// 外加凭据脱敏工具（[MaskSecret] / [RedactSecrets]）。
//
// 这是 Dart 版 conatus_core 的对等移植：语义对齐（同一套不变式、同一套验证
// oracle），API 形态按 Go 惯例重排：
//
//   - 撤销函数是 [Disposer]（`func() error`），Dart 用 `void Function()` + 异常；
//   - 可预期的失败走显式 error 返回，Dart 用 `throw`；
//   - 服务查找的泛型入口是自由函数 [Get] / [Require]——Go 方法不能自带类型参数；
//   - 构造参数非法（`maxRounds <= 0`）用 panic 表达编程错误，与 time.NewTicker 一致。
//
// # 三条硬保证（Dart 版语义，逐一由测试钉住）
//
//   - 幂等：重复 [EffectScope.Dispose] 只真正释放一次。
//   - 迟到安全：作用域释放后再 [EffectScope.Track]，撤销函数**立即执行**而不是
//     被静默丢弃。
//   - 抗异常：单个撤销返回错误或 panic 都不阻断其余撤销，全部错误聚合返回。
//
// # 并发契约
//
// 所有类型可被多 goroutine 并发使用。锁只在读写内部状态时持有，并且**绝不在持锁
// 时调用外部代码**（撤销函数、监听器、Inject 回调、Plugin 安装函数），因此回调中
// 重入 Track / Dispose 不会死锁。[Reactor.Notify] 不等待其他 goroutine：广播期间
// 从别的 goroutine 进入的变更只置脏标记后立即返回，由正在广播的那一轮负责再跑
// 一轮收敛。
//
// 包内零第三方依赖，只用标准库。
package core
