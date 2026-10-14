# AGENTS.md — conatus_go 项目指南

> 面向接手本项目的 AI 编码代理与开发者。读完本文档应能独立构建、测试、修改本仓库。
> 设计取舍与移植结论见 `docs/specs/2026-10-14-conatus-core-go-design.md`；
> 工作量调研与分档见根目录 `Go版本移植工作量调研.md`。

## 1. 项目概述

本仓是 **conatus 的 Go 实现**。Dart 原版（`github.com/Fi2zz/conatus`）是
「时空可组合性」编程范式的实现，同时是一个通用 Agent 框架；本仓的目标是把同一套
语义与同一套验证强度搬到 Go。

当前状态：**只有 `core` 包**（T0 spike，对应 Dart 的 `conatus_core`）：
可逆效应（`EffectScope`）、反应式共效应（`Reactor`）、统一上下文（`Context`）、
凭据脱敏。T1（foundation / llm / agent 等）未开始。

三条硬保证（改动 `core` 前必须理解，且必须由测试钉住）：

- **可逆效应**：每次副作用登记撤销函数，`EffectScope` 按 **LIFO** 回滚；幂等、
  迟到登记安全（释放后再 `Track` 会**立即执行**）、抗异常（单个逆返回错误或 panic
  都不阻断其余）。
- **反应式共效应**：`Inject` 声明依赖，依赖满足时在**全新子上下文**中激活，依赖
  消失时停用并撤销该子上下文的全部效应。
- **上下文树**：服务沿父链向下可见，子级服务对父级不可见；父级释放级联释放子树。
  `Reactor` 用「脏标记 + 重跑」把连锁反应在一次 `Notify` 内收敛，不收敛（循环依赖）
  返回 `ErrNotConverged` 快速失败，绝不静默死循环。

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
│   ├── errors.go             全树共享的错误接收器
│   └── *_test.go             单元 + 对等可逆性验证（外部测试包 core_test）
├── tool/
│   ├── verify_reversibility.sh   六层一键验证（fail-closed）
│   └── mutate_reversibility.sh   7 个变异体注入 + 杀死判定（fail-closed）
└── docs/specs/               设计文档（含映射结论、未覆盖分支清单）
```

## 3. 技术栈与依赖约束

- Go 1.22 起（用到泛型与 `atomic.Bool`/`atomic.Uint64`）。
- **零第三方依赖**：只用标准库。新增依赖需先确认真的必要——这是 Dart 版
  `conatus_core` 的卖点，Go 版沿用。
- 测试不用断言库，只用标准库 `testing`（对齐 Dart 侧「不用 mock 框架、用手写替身」）。

## 4. 常用命令

```bash
go build ./... && go vet ./...            # 构建与静态检查
gofmt -l .                                # 格式检查（输出非空即失败）
go test ./... -count=1                    # 单元测试
go test ./... -count=1 -race              # 竞态检测
go test ./... -count=1 -shuffle=42        # 随机顺序
bash tool/verify_reversibility.sh         # 六层一键验证（提交前必须通过）
bash tool/mutate_reversibility.sh         # 变异测试（改 core 后必须跑）
```

`go` 可能只装在 `/usr/local/go/bin` 未进 PATH；两个 tool 脚本会自己探测一次。

## 5. 代码组织与风格

- **一个文件一件事**：`services.go` 只放服务查找与登记、`inject.go` 只放共效应状态机、
  `context.go` 只放生命周期与装配。`core/` 内公开 API 平铺（`core.Context` 等）。
- **硬性约束**：单文件 ≤ 200 行、单函数 ≤ 40 行、函数参数 ≤ 4 个；单函数内
  `if / else / switch / case` ≤ 3、嵌套 ≤ 2、单个条件连续 `&& / ||` ≤ 2；优先 early return。
  例外需加 `// REASON: ...`（现有例子：`applyEffect` 的六路映射表）。
- **布尔命名不用 `is / has / can / should` 前缀**（用 `disposed` / `cancelled` /
  `active` / `activated` 这类状态词）；函数名「动词 + 名词」；禁止 `flag` / `temp` 弱命名。
- **注释用中文**，解释「为什么」而不是「做什么」。面向用户的文案/错误信息用中文。
- `Disposer` 是 `func() error`；无错清理用 `core.Ignore(func())` 适配，不要为了凑类型
  吞掉错误。

## 6. Go 侧与 Dart 版的形态差异（改 API 前必读）

这几条是移植的硬结论，不是风格偏好，T1 会继续沿用：

1. **移除监听器只能靠句柄**：`Reactor.Add` 返回 `*ListenerHandle`，没有
   `Remove(listener)`。Go 函数值不可比较，`reflect.Value.Pointer()` 无法区分同一
   字面量的不同闭包——按码指针移除会误删别的 inject。
2. **失败走显式 error**：可预期失败（服务重复/不可用/类型不符、install 失败、广播
   不收敛）返回 error；编程错误（`maxRounds <= 0`、nil 监听器、nil Disposer）panic。
3. **服务查找的泛型入口是自由函数**：`core.Get[T](ctx, key)` / `core.Require[T](ctx, key)`
   （Go 方法不能自带类型参数），底层是 `(*Context).Service(key) (any, bool)`。
4. **所有类型并发安全**，且 **绝不持锁调用外部代码**（撤销函数、监听器、inject 回调、
   install）。回调里重入 `Track` / `Notify` / `Dispose` 必须不死锁。
5. **错误接收器显式化**：inject 回调的错误在广播期间产生、无法同步冒泡，交给
   `SetErrorHandler`（默认写 stderr）；接收器由 root 持有、全树共享。
6. **不再有运行时类型分派**：Dart 的 `capture<T>` 拆成 `Capture`（登记返回值）与
   `Run`（不登记）两个显式方法。

## 7. 测试策略

| 层级 | 位置 | 关注点 |
|------|------|--------|
| 单元测试 | `core/*_test.go` | 单类型语义（服务、效应、收敛、共效应） |
| 对等可逆性验证 | `core/reversibility_*_test.go` | 论文 Theorem 16 的逐前缀还原 oracle + 负向对照 + 元检查 |
| 真资源探针 | `core/reversibility_{os_resources,goroutine}_test.go` | 真文件句柄（`lsof`）、goroutine 泄漏、真实 tick 停表 |

- 测试一律用外部测试包 `core_test`，只经公开 API 验证行为。
- 验证夹具（`effectWorld` / `reversibilityHarness`）自己也要自检：三种失败模式
  （安慰剂逆 / 漏登记 / 半个逆）必须被构造成不同形态，且负向对照必须被检出。
- **改 `core` 的效应或上下文机制后必须跑 `tool/verify_reversibility.sh`**，
  尤其是变异测试——它是「测试是否真的在断言」的唯一证据。
- 覆盖率仅作信息性输出；当前有 5 行并发防御分支未覆盖，原因见设计文档 §7。

## 8. 开发约定

- 提交信息：Conventional Commits + 中文描述 + scope，例：
  `feat(core): 新增 Inject 共效应状态机`、`fix(core): 修正 LIFO 撤销顺序`。
  功能 / 修复 / 重构分开提交，不把「顺手优化」混进来。
- 收尾顺序：`gofmt -l .` 无输出 → `go vet ./...` → `go test ./... -count=1 -race`
  → `bash tool/verify_reversibility.sh` 总判定通过 → 提交。
- 不手动修改生成物（本仓暂无生成物）。`.handoffs/`（若存在）用于跨轮次交接。

## 9. 已知边界

- 只有 `core`：任何 T1 的包都**尚未实现**，本文档不写未定论的推测。
- `Provide` 的非收敛错误返回目前不可达（public API 挂不上任意监听器），保留为防御。
- `lsof` 不存在时 OS 级 FD 用例自动跳过（fail-open 仅在「工具缺失」这一种情况，
  其余一律 fail-closed）。
- 未接 CI；提交前靠本地 `tool/verify_reversibility.sh` 把关。
