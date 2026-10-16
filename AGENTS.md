# AGENTS.md — conatus_go 项目指南

> 面向接手本项目的 AI 编码代理与开发者。读完本文档应能独立构建、测试、修改本仓库。
> 设计取舍与移植结论见 `docs/specs/2026-10-14-conatus-core-go-design.md`；
> 工作量调研与分档见根目录 `Go版本移植工作量调研.md`。

## 1. 项目概述

本仓是 **conatus 的 Go 实现**。Dart 原版（`github.com/Fi2zz/conatus`）是
「时空可组合性」编程范式的实现，同时是一个通用 Agent 框架；本仓的目标是把同一套
语义与同一套验证强度搬到 Go。

当前进度（T1 分 15 个里程碑推进，见设计文档 §四）：

| 里程碑 | 状态 |
|--------|------|
| M0 验证基建泛化 | ✅ |
| M1a foundation 小件：`home` / `logging` / `timer` | ✅ |
| M1b 工具系统（`foundation/tools`） | ✅ |
| M1c 会话事件日志（`foundation/session`） | ✅ |
| M1a 其余小件：`loader` / `ask` | ✅ |
| M1d system prompt 与时间锚点（`foundation/prompt`） | ✅ |
| M2 `llm`：类型层 / 契约 / 失败分类 / 服务装配 / 退避策略 | ✅ |
| M2 `llm`：OpenAI 兼容 wire 层（chat + responses 双形态，SSE 解析为纯函数） | ✅ |
| M2 `llm`：`Retrying` / `Fallback` 两个装饰器 | ✅ |
| M3 `agent`：最小闭环（循环 + 事件派生 + 取消） | ✅ |
| M3 `agent` 其余插件（compaction / memory / reflection / router / plan / goal / telemetry …） | ⬜ |
| M2 其余（store / fs / shell / sessionlog / …） | ⬜ 未开始 |

三条硬保证（改动 `core` 前必须理解，且必须由测试钉住）：

- **可逆效应**：每次副作用登记撤销函数，`EffectScope` 按 **LIFO** 回滚；幂等、
  迟到登记安全（释放后再 `Track` 会**立即执行**）、抗异常（单个逆返回错误或 panic
  都不阻断其余）。
- **反应式共效应**：`Inject` 声明依赖，依赖满足时在**全新子上下文**中激活，依赖
  消失时停用并撤销该子上下文的全部效应。
- **上下文树**：服务沿父链向下可见，子级服务对父级不可见；父级释放级联释放子树。
  `Reactor` 用「脏标记 + 重跑」把连锁反应在一次 `Notify` 内收敛，不收敛（循环依赖）
  返回 `ErrNotConverged` 快速失败，绝不静默死循环。

工具系统的三条不变式（改 `foundation/tools` 前必读）：

- 面向模型只暴露白名单投影（`name` / `description` / `parameters`），宿主字段
  （风险等级、分组、超时、路径参数、执行体）**永不下发**。
- 未知工具 / 参数不合法 / 守卫拒绝 / 超时 / 执行体失败一律收敛为**失败结果**，
  `Registry.Call` 不返回 error。
- **超时只包住工具体**，不套住中间件——审批之类的中间件在等用户（可达数分钟）。
  混在一处计时时小的那个静默获胜：用户批准后工具照样执行，但模型已被告知失败，
  写操作会变成「以为失败而重试，实际写了两遍」。

## 2. 仓库结构

```
├── go.mod                    module github.com/Fi2zz/conatus_go（零第三方依赖）
├── core/                     时空可组合性内核（= Dart 的 conatus_core）
│   ├── doc.go                包文档 + 并发契约（改 API 先读它）
│   ├── effect_scope.go       Disposer / EffectScope / Ignore
│   ├── reactor.go            Reactor / ListenerHandle / ErrNotConverged
│   ├── services.go           Provide / Service / Get / Require / Has
│   ├── context.go            Context / NewRoot / Plugin / 生命周期 / 错误接收器
│   ├── inject.go             Inject（共效应状态机）
│   ├── redaction.go          IsSensitiveKey / MaskSecret / RedactSecrets
│   └── errors.go             全树共享的错误接收器
├── foundation/
│   ├── home/                 用户目录解析 / UUID v4 / 落盘权限收紧
│   ├── logging/              分级日志服务 + 控制台导出器 + Provide
│   ├── timer/                可逆定时器：Timeout / Interval / Sleep / Throttle / Debounce
│   ├── tools/                工具系统：ParamSpec→JSON Schema / 校验 / 注册表 / 执行管线
│   ├── session/              会话事件日志：append-only / fork / JSONL 同步落盘
│   ├── loader/               插件装载器（层级 id / 级联卸载）
│   ├── ask/                  提问服务（等待用户回答，可撤销）
│   └── prompt/               system prompt 段装配 + 日粒度时间锚点
├── llm/                      统一的大模型接入层（= Dart 的 conatus_llm）
│   ├── types.go              消息 / 工具调用 / 图片 / 结果 / 流式事件（+ JSON 投影）
│   ├── provider.go           Provider 契约 + Option + StreamChatResult 累积器 + 流式泵
│   ├── errors.go             Error / ErrorKind（状态码推断 + 可重试判定）
│   ├── retry_policy.go       退避策略（次数 / 上限 / 抖动 / 服务端 Retry-After）+ Attempt
│   ├── retry.go              Retrying 装饰器（退避与睡眠均可注入）
│   ├── fallback.go           Fallback 回退链（换提供商 + 回退事件通报）
│   ├── openai.go             OpenAI 兼容 provider（选项 / 端点 / 生命周期）
│   ├── openai_http.go        请求构造、状态码错误、错误正文提取
│   ├── openai_stream.go      流式驱动（拉帧 → 产出事件 → 终态）
│   ├── sse.go                SSE 逐行解析（纯函数：吃 io.Reader，可脱离网络单测）
│   ├── stream_frame.go       两种形态的帧 → 事件
│   ├── stream_accum.go       工具调用分片累积（保序）
│   ├── request_body.go       两种形态的请求体与工具 schema 投影
│   ├── retryafter.go         `Retry-After` 解析（HTTP-date 走可注入时钟）
│   └── json.go               JSON 取值归一
├── agent/                     Agent 主循环（= Dart 的 conatus_agent，目前只有最小闭环）
│   ├── types.go              包文档 + SteeringSource / Observer / TurnTracker / Step / Turn
│   ├── loop.go               Loop 装配、Run、轮次钩子与事件写入
│   ├── round.go              单轮状态机：步进 → 工具调用回填 → 收口
│   ├── events.go             会话事件 ↔ 模型消息（历史不另存，全靠事件派生）
│   ├── system.go             system 文本装配（prompt 段 + 动态上下文）
│   ├── cancel.go             取消语义（ctx 原生取消 + ErrCancelled）
│   └── provide.go            装配到上下文（服务键 `agentLoop`）
├── tool/
│   ├── lib.sh                六层验证的共用实现
│   ├── verify_all.sh         全模块一键验证（可传路径前缀收窄）
│   ├── verify_reversibility.sh  core 专项入口（= verify_all.sh core）
│   ├── mutants.txt           变异体清单（域|标签|目标文件|原文|替换）
│   └── mutate.sh             变异测试执行器（fail-closed，兼容 macOS bash 3.2）
└── docs/specs/               设计文档（含 T1 定论、映射结论、未覆盖分支清单）
```

## 3. 技术栈与依赖约束

- Go 1.23 起（用到泛型、`atomic.Bool`/`atomic.Uint64`、为 `iter.Seq2` 预留）。
- **零第三方依赖**：只用标准库。新增依赖需先确认真的必要——这是 Dart 版
  `conatus_core` 的卖点，Go 版沿用（T1 的 YAML frontmatter 与文件监听也走自写/降级
  方案，见设计文档 D-T1-8）。
- 测试不用断言库，只用标准库 `testing`（对齐 Dart 侧「不用 mock 框架、用手写替身」）。
- tool 脚本必须兼容 **macOS 自带的 bash 3.2**：不用数组、不用 `mapfile`。

## 4. 常用命令

```bash
go build ./... && go vet ./...            # 构建与静态检查
gofmt -l .                                # 格式检查（输出非空即失败）
go test ./... -count=1 -race              # 单元测试 + 竞态检测
bash tool/verify_all.sh                   # 全模块六层验证（提交前必须通过）
bash tool/verify_all.sh foundation/tools  # 只验某块（覆盖率与变异同步收窄）
bash tool/mutate.sh core                  # 只跑某域的变异体
```

`go` 可能只装在 `/usr/local/go/bin` 未进 PATH；脚本会自己探测一次。

## 5. 代码组织与风格

- **一个文件一件事**：`services.go` 只放服务查找与登记、`inject.go` 只放共效应状态机、
  `pipeline.go` 只放执行管线。公开 API 平铺在各自包里（`core.Context`、`tools.Registry`）。
- **一个插件一个 Go 包**：不照搬 Dart「一个包塞 42 个文件」——Go 的包是依赖单元，
  未来 `session` 的使用者不该被迫拉进 `shell`。
- **硬性约束**：实现文件 ≤ 200 行（**测试文件放宽到 ≤ 300 行**，用例密度优先，对齐
  Dart 仓现状）、单函数 ≤ 40 行、函数参数 ≤ 4 个；单函数内
  `if / else / switch / case` ≤ 3、嵌套 ≤ 2、单个条件连续 `&& / ||` ≤ 2；优先 early return。
  例外需加 `// REASON: ...`（现有例子：`applyEffect` 六路映射表、`checkValue` 七类型表、
  `asNumber` 数字归一表）。
- **布尔命名不用 `is / has / can / should` 前缀**（用 `disposed` / `cancelled` /
  `active` / `activated` 这类状态词）；函数名「动词 + 名词」；禁止 `flag` / `temp` 弱命名。
- **注释用中文**，解释「为什么」而不是「做什么」。面向用户的文案/错误信息用中文。
- **选项函数而非长参数表**：`NewRegistry(WithDefaultTimeout(d))`、
  `StringParam("q", Required(), Describe("…"))`。同名选项在不同包里各自命名
  （如 `WithFn*` / `WithConsole*` / `WithCallTimeout`）以避免包内冲突。
- **重复出现的机制抽成可复用件**：`core.Ignore`（无错清理）、`tools.slotList[T]`
  （登记→可移除）、`tools.optional` / `asInteger`（取值归一）。

## 6. Go 侧与 Dart 版的形态差异（改 API 前必读）

这几条是移植的硬结论，不是风格偏好：

1. **移除监听器只能靠句柄**：`Reactor.Add` 返回 `*ListenerHandle`；注册表、守卫、
   中间件、监听器的撤销一律走句柄（`tools.slotList`），**绝不**用
   `reflect.Value.Pointer()` 比函数——同一字面量的不同闭包会共享码指针，会误删别人。
2. **失败走显式 error**：可预期失败（服务重复/不可用/类型不符、install 失败、广播
   不收敛、工具重名）返回 error；编程错误（`maxRounds <= 0`、nil 监听器、nil Disposer、
   空枚举声明）panic。
3. **泛型入口是自由函数**：`core.Get[T](ctx, key)` / `core.Require[T](ctx, key)`、
   `tools.Get[T](tc, name)` / `tools.Require[T](tc, name)`——Go 方法不能自带类型参数。
   Dart 的 `ctx.tools` 这类扩展方法对应 `tools.From(ctx)`。
4. **所有类型并发安全**，且 **绝不持锁调用外部代码**（撤销函数、监听器、inject 回调、
   守卫、中间件、工具执行体、日志导出器）。回调里重入登记/撤销/调用必须不死锁。
5. **错误接收器显式化**：inject 回调与释放路径的错误无法同步冒泡，交给
   `SetErrorHandler`（默认写 stderr）；接收器由 root 持有、全树共享。
6. **不再有运行时类型分派**：Dart 的 `capture<T>` 拆成 `Capture`（登记返回值）与
   `Run`（不登记）；工具元数据用「最小接口 + 可选接口」（`RiskOf` / `TimeoutOf` /
   `PathParamsOf` / `ParamsOf` / `GroupOf`），简单工具只写三个方法。
7. **数字取值放宽**：Dart 的 `jsonDecode` 保留 int/double 之分（所以那边
   `value is int` 成立），Go 的 `encoding/json` 只有 float64。因此 `Integer` /
   `ParamInteger` 接受「任何整数值的数字」（含 `json.Number`），见 `coerce.go`。
8. **时间敏感件必须可注入时钟**（`WithClock` / `WithThrottleClock`），禁止裸
   `time.Now()`——schedule / cron / autonomous 的确定性测试全靠它。
9. **定时器与工具回调跑在独立 goroutine 上**：Dart 的单线程事件循环没有这个差异，
   Go 侧对照测试里共享的计数器必须用原子量，否则 `-race` 会（正确地）报竞争。

## 7. 测试策略

| 层级 | 位置 | 关注点 |
|------|------|--------|
| 单元测试 | `core/*_test.go`、`foundation/*/*_test.go` | 单类型语义（服务、效应、收敛、共效应、日志、定时器、工具管线） |
| 对等可逆性验证 | `core/reversibility_*_test.go` | 论文 Theorem 16 的逐前缀还原 oracle + 负向对照 + 元检查 |
| 真资源探针 | `core/reversibility_{os_resources,goroutine}_test.go` | 真文件句柄（`lsof`）、goroutine 泄漏、真实 tick 停表 |

- 测试一律用外部测试包（`core_test` / `tools_test` …），只经公开 API 验证行为。
- 验证夹具（`effectWorld` / `reversibilityHarness`）自己也要自检：三种失败模式
  （安慰剂逆 / 漏登记 / 半个逆）必须被构造成不同形态，且负向对照必须被检出。
- **改机制后必须跑 `bash tool/verify_all.sh`**，尤其是变异测试——它是「测试是否真的
  在断言」的唯一证据。新增实现请同步在 `tool/mutants.txt` 加条目（模式必须是单行、
  在目标文件里唯一）。
- 覆盖率仅作信息性输出（各文件 60%~100%），真正的闸门是变异测试。缺的通常是同类
  分支的其它形态（如其它类型的取值函数）与防御分支。

## 8. 开发约定

- 提交信息：Conventional Commits + 中文描述 + scope，例：
  `feat(foundation): 新增 home/logging/timer 三个基础子包`、
  `fix(core): 修正 LIFO 撤销顺序`。功能 / 修复 / 重构分开提交，不把「顺手优化」混进来。
- 收尾顺序：`gofmt -l .` 无输出 → `go vet ./...` → `go test ./... -count=1 -race`
  → `bash tool/verify_all.sh` 总判定通过 → 提交。
- 不手动修改生成物（本仓暂无生成物）。`.handoffs/`（若存在）用于跨轮次交接。

## 9. 已知边界

- **已落地**：`core` + `foundation/{home,logging,timer,tools,session,loader,ask,prompt}`
  + `llm`（类型 / 契约 / 失败分类 / 退避策略 / wire 层 / 重试与回退装饰器）
  + `agent` 的**最小闭环**（循环 + 事件派生 + 取消 + 装配）。
- `agent` 只做了最小闭环：Dart 侧的 compaction / memory / reflection / router / plan /
  goal / telemetry / approval / sub-agent 都**还没落地**，因此 `Loop` 上没有它们的字段与
  分支。别按「agent 已完成」续写。
- 本轮修掉 Dart 侧一个会丢输入的坑（已写进 `Loop.openingMessages` 的注释）：Dart 那份的
  messages 完全由事件日志派生，于是**没绑定会话（临时对话）时用户输入根本发不出去**
  ——日志里就没有那条事件。Go 侧显式带上本轮用户消息，历史在写事件之前抄好，不会重复。
- 取消语义与 Dart 不同：Dart 用 `AgentCancel` + `Completer` 竞速「停止等待」（Future 杀不掉）；
  Go 的 ctx 会真正取消在途工作（HTTP 请求与工具执行体都拿得到它），所以循环只看 ctx，
  统一返回 `agent.ErrCancelled`。代价：无视 ctx 的第三方工具会让这一轮多等一会。
- `llm` 已完整可用（两种形态的 wire 层 + 重试装饰器 + 回退链）；只剩**凭据服务**
  （构造期解析 + 运行期轮换）未落地——provider 显式收 `apiKey`，不读环境变量（与 Dart 同语义）。
- 重试与回退共守一条流式约束：**已经交出去的增量收不回来**，故只在「尚未产出任何事件」时
  重试或换提供商，已产出后一律上抛。退避睡眠认 ctx（Dart 的 `Future.delayed` 打断不了），
  取消时立刻返回，不会把已放弃的请求按在原地。
- `llm.OpenAI` 的超时只落在**自建**客户端上（`ResponseHeaderTimeout`，只包住等响应头阶段）；
  注入 `WithHTTPClient` 后 `WithTimeout` 不生效——Go 没有「只限响应头」的 client 级开关，
  本包也不去改调用方给的客户端。
- `core.Provide` 的非收敛错误返回目前不可达（public API 挂不上任意监听器），保留为防御；
  同理 `tools` 里「撤销时按槽位指针比对」的分支在单线程用例下不可达（并发交错下才走到）。
- 工具超时的语义是「丢弃迟到结果」而非取消：无视 ctx 的工具会继续在后台跑完，
  结果被丢弃（缓冲通道保证 goroutine 不泄漏）——与 Dart 的 `Future.timeout` 一致。
- `lsof` 不存在时 OS 级 FD 用例自动跳过（fail-open 仅在「工具缺失」这一种情况，
  其余一律 fail-closed）。
- 未接 CI；提交前靠本地 `tool/verify_all.sh` 把关。
