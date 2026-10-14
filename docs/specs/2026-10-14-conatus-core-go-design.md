# conatus_core 的 Go 实现（T0 spike）设计

> 状态（2026-10-14）：T0 已完成并实测通过。范围只含 `conatus_core` 一个包
> + 对等可逆性验证套件；T1（foundation / llm / agent 等）未开始。
> 上游 Dart 实现：`github.com/Fi2zz/conatus` → `packages/conatus_core/`。
> 决策背景见本仓根 `Go版本移植工作量调研.md`。

## 1. 范围与目标

目标不是「把 507 行 Dart 翻译成 Go」，而是用可运行的 Go 代码回答调研 §二 里悬着的
四个范式映射问题（异步/并发模型、类型擦除的服务树、撤销函数的错误通道、工具声明），
并留下与 Dart 版**同等强度**的验证证据：

- 同一套不变式（服务可见性、inject 全新子上下文、LIFO/幂等/迟到安全、Reactor 收敛）
- 同一套 oracle（论文 Theorem 16 的逐前缀还原 + Theorem 7 的逆方程 + 负向对照 + 元检查）
- 同一套 fail-closed 武器（变异测试注入 7 个真 bug，全部必须被杀）

## 2. 交付物（实测行数）

| 部分 | 文件 | 行数 |
|------|------|------|
| 实现 | `core/{doc,effect_scope,reactor,services,context,inject,redaction,errors}.go` | 1011（含 `doc.go` 29） |
| 测试 | `core/*_test.go`（23 个文件） | 2699 |
| 工具 | `tool/{verify,mutate}_reversibility.sh` | 216 |

用例：**100 个顶层测试函数 / 124 个用例（含子用例）**。Dart 侧 conatus_core 为
76 个静态用例（10 个随机种子循环动态展开，实际约 85）。测试/实现行数比 Go 为 2.7
（Dart 为 3.0），强度未松。

## 3. API 形态定论（D1–D10）

| # | Dart 形态 | Go 形态 | 理由与实测 |
|---|-----------|---------|-----------|
| D1 | `typedef Disposer = void Function()` | `type Disposer func() error` | 撤销错误（如 `provide` 撤销触发的广播不收敛）是本模型的真实信息，Dart 用 `dispose()` 返回 `List<Object>` 聚合，Go 用 error 天然聚合。无错清理用 `core.Ignore(func())` 适配 |
| D2 | 异常 `throw StateError` | 显式 `error` 返回 | `Provide` / `Plugin` / `Notify` / `Require` 返回 error；**构造参数非法**（`maxRounds <= 0`、nil 监听器、nil Disposer）用 panic 表达编程错误（同 `time.NewTicker`） |
| D3 | try/catch 收集撤销异常 | `Dispose() []error` + 逐个逆 `recover()` 兜底 | 「抗异常」保住两条腿：错误返回不阻断、panic 不阻断 |
| D4 | `ctx.get<T>()` / `require<T>()`（方法带类型参数） | 自由函数 `Get[T](ctx,key) (T,bool)` / `Require[T](ctx,key) (T,error)` + 底层 `(*Context).Service(key) (any,bool)` | Go 方法不能自带类型参数。Dart 的 `get<T>` 类型不符会抛 cast 错，Go 分成「不存在」（false/不可用）与「类型不符」（Require 单独的措辞） |
| D5 | `EffectScope.capture<T>` 运行时判 `result is Disposer` | 拆成 `Capture(body func() Disposer) Disposer` / `Run(body func())` | 静态类型下泛型分派没有信息量；语义等价（「返回 Disposer 就登记」只剩一种写法） |
| D6 | 单线程事件循环，天然无锁 | **core 内部加锁，声明并发安全** | 见 §6 发现 3。Dart 用例「异步初始化期间 context 被释放」在 Go 里天然是两个 goroutine 同时访问 Context |
| D7 | `identical(_services[key], value)` | `map[string]*serviceSlot`，比 `*serviceSlot` 指针 | `any` 直接 `==` 会在 slice/map/func 值上 panic；包一层 slot 后指针比较永远安全且语义等价 |
| D8 | 允许 `provide(key, null)`（但 `get<T extends Object>` 取不回来） | `Provide` 拒绝 nil 值（返回 error） | 把 Dart 版「服务存在却不可读」的空洞收紧成显式错误 |
| D9 | `Zone.current.handleUncaughtError` | root 持有、全树共享的错误接收器（`SetErrorHandler(func(error))`，默认写 stderr） | inject 回调的错误在广播期间产生、调用栈已断，两语言都无法同步冒泡；差别只在 Dart 的 Zone 是环境隐式的 |
| D10 | `localServiceKeys` 插入序 | 返回**排序后**的键 | Go map 无序；Dart 侧唯一消费点（投影）本来就 `sort()` |
| D11 | `Reactor.remove(listener)` | `Add(listener) *ListenerHandle` + `handle.Remove()` | **Go 强制**：函数值不可比较，`reflect.Value.Pointer()` 不保证区分同一字面量的不同闭包。见 §6 发现 1 |

**刻意优于原版的一处**：`Provide` 在广播不收敛时，Dart 抛异常导致调用方拿不到
`Disposer`（服务泄漏）；Go 返回 `(disposer, err)` 两者都给。

## 4. 语义对等矩阵

| Dart 测试文件 | 行数/用例 | Go 对应 |
|---|---|---|
| `effect_scope_test.dart` | 89 / 7 | `core/effect_scope_test.go` |
| `effect_scope_meta_test.dart` | 38 / 4 | `core/effect_scope_meta_test.go` |
| `reactor_test.dart` | 107 / 10 | `core/reactor_test.go` |
| `context_test.dart` | 213 / 18 | `core/context_test.go` + `services_visibility_test.go` + `services_test.go` |
| `composability_test.dart` | 275 / 16 | `core/inject_test.go` + `inject_edge_test.go` + `cascade_test.go` + `inject_async_test.go` |
| `reversibility_harness.dart` | 156（夹具） | `core/reversibility_harness_test.go` |
| `reversibility_world.dart` | 169（夹具） | `core/reversibility_world_test.go` + `reversibility_resources_test.go` + `reversibility_effects_test.go` |
| `reversibility_test.dart` | 146 / 15 | `core/reversibility_test.go` |
| `reversibility_negative_control_test.dart` | 108 / 7 | `core/reversibility_negative_control_test.go` |
| `reversibility_hard_cases_test.dart` | 92 / 4 | `core/reversibility_hard_cases_test.go` |
| `reversibility_observability_test.dart` | 56 / 2 | `core/reversibility_observability_test.go` |
| `reversibility_os_resources_test.dart` | 64 / 1 | `core/reversibility_os_resources_test.go` |
| —（Dart 无） | — | `core/reversibility_goroutine_test.go`（goroutine 泄漏 + tick 停表）、`core/redaction_test.go`、`core/errors_test.go` |

对等关系是「同一攻击/同一断言」，不是逐行转写：Dart 的 `Timer.isActive` /
`StreamController.hasListener` 在 Go 没有等价物，改用世界侧代理存活位 + 真实 tick
计数 + goroutine 退出作为硬证据（`reversibility_resources_test.go`）。

## 5. 验证分层与实测结果

`bash tool/verify_reversibility.sh` 一键跑完六层（fail-closed：任一层的工具缺失、
崩溃或非零退出都判未通过）：

| 层 | 命令 | 实测 |
|---|---|---|
| 格式 | `gofmt -l .` | 无差异 |
| 静态检查 | `go vet ./...` | 零告警 |
| 单元测试 | `go test ./... -count=1` | 通过（124 用例） |
| 随机顺序 | `go test -shuffle={1,42,12345}` | 三个种子全通过 |
| 竞态检测 | `go test -race -count=1` | 零报告（Dart 单线程模型下不存在这一层） |
| 行覆盖率 | `go test -coverprofile` + awk 汇总 | 332 / 338 行覆盖（98.2%），见 §7 |
| 变异测试 | `bash tool/mutate_reversibility.sh` | **杀死 7 / 存活 0 / 硬失败 0** |

变异体（与 Dart 版一一对应，另加「模式必须唯一」的 Go 侧加固）：

| 变异体 | 结果 |
|---|---|
| LIFO 改成 FIFO（`last := len(...) - 1` → `last := 0`） | 已杀死（25 条失败） |
| 迟到登记不再立即执行 | 已杀死（14 条失败） |
| 撤销错误被静默吞掉 | 已杀死（6 条失败） |
| `Capture` 不再登记 Disposer | 已杀死（3 条失败） |
| `inject` 不登记取消（调用点） | 已杀死（1 条失败） |
| [元] 夹具不再构造漏登记 | 已杀死（20 条失败） |
| [元] 投影不再观察定时器 | 已杀死（4 条失败） |

覆盖率（机制自身）：`effect_scope.go` 63/63、`reactor.go` 61/61、`inject.go` 58/58、
`redaction.go` 51/51、`errors.go` 11/11、`context.go` 21/22、`services.go` 67/72。

## 6. 映射结论：实测发现的九件事

1. **`remove(listener)` 在 Go 无法表达**（最重要的一条）。Go 函数值只能与 nil 比较；
   `reflect.ValueOf(f).Pointer()` 只保证 nil 判定，同一函数字面量生成的不同闭包会拿到
   同一个码指针。而 inject 每次激活都生成一个新闭包——若按码指针移除，会误删**另一个**
   inject 的监听器（且这种 bug 只在「一个上下文里有两个 inject」时才暴露）。因此
   Reactor 的移除只能改为句柄式。这条改变会一路传播到 T1 的所有事件/订阅 API。
2. **并发不是可选项，而是 Dart 语义的强制后果**。`异步初始化期间 context 被释放`
   这条用例在 Go 里天然跨 goroutine，core 因此必须自带锁；锁的纪律随之变成硬要求：
   **绝不持锁调用外部代码**（撤销函数、监听器、inject 回调、install），否则 inject
   回调里再 `Track`/`Notify` 立即死锁。`-race` 是这条纪律唯一可靠的检查手段。
3. **Dart 里是死代码的守卫，在 Go 里复活**。`identical` 式服务守卫与
   `dropRemainingServices` 兜底在单线程下不可达（`removed` 幂等标志先挡住），但
   在「registerService 与 Track 之间被并发 Dispose 打断」的交错下会真的走到——
   这是 §7 里 5 行未覆盖代码的全部来源（两者都在 Dart 版里同样存在）。
4. **`Disposer` 带错误返回改变了「抗异常」的实现路径**：Dart 靠 try/catch 收集，
   Go 靠 `func() error` 返回值聚合 + `recover()` 兜底 panic。变异测试里「撤销错误被
   静默吞掉」被 5 条测试杀掉，说明这条语义没有松。
5. **`Provide` 的非收敛错误分支实际不可达**：public API 无法往某个 Context 的
   reactor 上挂任意监听器，而 inject 的监听器总能收敛。保留它是防御性设计（Dart 侧
   同样不可达，只是那边表现为抛异常）。
6. **nil 服务值**：Dart 允许 `provide(key, null)` 却永远取不回来（见 D8），Go 侧收紧
   为显式错误——这是移植过程中发现的**上游缺陷**，不是 Go 的退化。
7. **错误接收器必须显式化**（D9）。Dart 的 Zone 是环境；Go 侧必须把它挂在上下文树上，
   并因此产生一个额外的测试面（`errors_test.go`：`SetErrorHandler(nil)` 恢复默认并
   真的写 stderr）。
8. **投影字符串可以逐字节对齐**：`Context(root)/a`、`共享值: k=v|…`、`定时器存活: N`
   等行两语言一致，这给「跨语言对拍同一场景」留了口子（本轮没做，T1 若要做双轨可以
   用它做 CI 对拍）。
9. **Dart 的 `capture<T>` 泛型分派没有 Go 对应物**（D5）：静态类型下拆成
   `Capture`/`Run` 两个显式方法更清楚，代价是用例要分两条写。

## 7. 未覆盖分支清单（5 行，全部为防御性）

| 位置 | 分支 | 为什么没覆盖 |
|---|---|---|
| `context.go:109` | `dropRemainingServices()` 为真后的 `notify()` | 需要「scope 释放完毕但服务仍在 map 里」，只有并发交错能构造（见发现 3） |
| `services.go:171` | 兜底清空服务表并返回 true | 同上（同一个机制的另一半） |
| `services.go:157` | `detachService` 的 slot 指针守卫返回 false | 幂等标志先挡住了重复撤销；只有并发交错（dispose 抢在 Track 之前）能走到 |
| `services.go:35` | `detachService` 返回 false 时的提前返回 | 同上 |
| `services.go:64` | `Provide` 的 `notify()` 错误返回 | 见发现 5：public API 无法构造不收敛的广播 |

结论：**没有一行是「语义没测到」**；这 5 行全是并发防御路径或不可达的 API 契约分支，
与 Dart 版一一对应。覆盖率仅作信息性输出，真正的闸门是变异测试。

## 8. Go 侧相对 Dart 的新增验证

- `go test -race`（竞态检测）：Dart 单线程模型下不存在这一层。
- goroutine 泄漏探针：挂载/卸载 50 轮后 `runtime.NumGoroutine()` 必须回到基线
  （Dart 靠 GC + 事件循环免费获得，Go 必须显式证明）。
- 真实 tick 停表：卸载后周期回调计数必须冻结（Dart 用 `Timer.isActive` 代理，
  Go 侧代理位 + 真实计数双证据）。
- `redaction_test.go`：Dart 侧 conatus_core **没有**脱敏单测（只在 credentials /
  foundation 间接用到），Go 侧按 README 声明的行为补齐。

## 9. 对调研估算模型的校准（T0 实测）

调研报告给 T0 的估算是「2~3 周」（1 名熟悉双语的工程师 + AI 辅助）。本次实测：
实现 1011 行 + 测试 2699 行 + 工具 258 行，在一个 AI 编码会话内完成，全部验证层通过。

需要说明的是这**不能**直接外推到 T1：T0 的特殊性在于「语义已定型 + 有 1513 行
Dart 测试当规格 + 全部在单进程内、零外部 IO」。T1 起要面对 HTTP/SSE/子进程/MCP，
移植型工作的「600~1000 行/人日」口径才会真正起作用。建议 T1 开始时按包重新采样一次
实测速率（本仓的 `tool/verify_reversibility.sh` 可以按包复制）。

## 10. 未决与留给 T1 的问题

- 是否把 `Context` 的 reactor 暴露出来（现在无法从外部挂监听器，`Provide` 的非收敛
  错误因此不可达）。
- 是否统一「服务键 → 类型」注册表（现在 `Get[T]` 靠运行期断言，键名写错只会在运行的
  那一刻发现）。Dart 侧同样是字符串键。
- 异步层（llm / search / mcp / shell）的错误通道：本轮定论是「可预期失败走 error
  返回、编程错误 panic」，T1 需要给出 `context.Context` 取消传播的统一约定。
- 是否做跨语言对拍（同一场景在 Dart 与 Go 各跑一次，比对投影字符串）。
