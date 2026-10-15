# conatus_go

conatus 的 **Go 实现**——「时空可组合性（Spatiotemporal Composability）」编程范式：
可逆效应（时间可组合性）+ 反应式共效应（空间可组合性）统一在一个 `Context` 上，
之上是工具系统与（进行中的）Agent 能力。

对应 Dart 原版：[github.com/Fi2zz/conatus](https://github.com/Fi2zz/conatus)
（本仓的语义、测试 oracle 与验证强度都对齐它的 `packages/`）。

## 当前进度

零第三方依赖（只用标准库）。T1 按里程碑推进：

| 里程碑 | 状态 |
|--------|------|
| `core`（可逆效应 / 反应式共效应 / 上下文树 / 脱敏） | ✅ T0 spike |
| `foundation/home` 用户目录 / UUID / 落盘权限 | ✅ |
| `foundation/logging` 分级日志 + 控制台导出 | ✅ |
| `foundation/timer` 可逆定时器（Timeout/Interval/Sleep/Throttle/Debounce） | ✅ |
| `foundation/tools` 工具系统（ParamSpec→JSON Schema / 校验 / 注册表 / 执行管线） | ✅ |
| `foundation/session` 事件日志（快照读 / 同步落盘 JSONL / fork 后缀语义） | ✅ |
| `foundation/{loader,ask,prompt,store,fs,shell,sessionlog}` | ⬜ |
| `llm` / `compaction` / `agent` / `tasks` / `skill` / `search` / `credentials` / `schedule` | ⬜ |

分档与估算见 `Go版本移植工作量调研.md`，T1 的 15 个里程碑与架构定论见
[`docs/specs/2026-10-14-conatus-core-go-design.md`](docs/specs/2026-10-14-conatus-core-go-design.md)。

## 用法

```go
app := core.NewRoot("app")
stop, err := app.Provide("config", Config{Region: "cn-beijing"})
if err != nil {
    return err
}

registry, err := tools.Provide(app)
if err != nil {
    return err
}
off, err := registry.Fn("get_time", "读取当前时间",
    func(ctx context.Context, tc *tools.ToolContext) (tools.ToolResult, error) {
        return tools.Success(time.Now().Format(time.RFC3339)), nil
    },
    tools.WithFnRisk(tools.RiskLow),
)
if err != nil {
    return err
}
app.Track(off)

app.Inject([]string{"config", "tools"}, func(child *core.Context) error {
    // 依赖齐全时在全新子上下文中激活；任一依赖消失即整体撤销
    return nil
})

_ = stop()      // config 消失 → 依赖它的注入自动停用，子上下文效应全部 LIFO 回滚
app.Dispose()
```

## 验证

```bash
go test ./... -count=1 -race          # 单元 + 竞态
bash tool/verify_all.sh               # 六层一键验证（提交前必须通过）
bash tool/mutate.sh                   # 变异测试：15 个真 bug 必须全部被杀
```

`go` 若只装在 `/usr/local/go/bin` 未进 PATH，脚本会自己探测一次。

验证强度对齐 Dart 版：Theorem 16 的逐前缀还原 oracle、Theorem 7 的逆方程、三种假可逆的
负向对照，以及「测试是否真的在断言」的变异测试（含 2 个针对夹具本身的元检查）。
Go 侧另外加了 `-race`、goroutine 泄漏探针、真实 tick 停表与真文件句柄（`lsof`）计数。

## 与 Dart 版的形态差异（硬结论）

- 移除监听器用句柄（`*ListenerHandle`）：Go 函数值不可比较，按码指针移除会误删别的回调。
- 失败走显式 `error` 返回；构造参数非法才 panic。
- 泛型服务/参数查找是自由函数：`core.Get[T]` / `tools.Require[T]`。
- 工具声明沿用声明式 `ParamSpec`（不引反射），元数据用「最小接口 + 可选接口」。
- **工具超时只包住工具体**，不套住审批这类等用户的中间件。
- 所有类型并发安全，且**绝不持锁调用外部代码**（回调里重入不会死锁）。

完整决策表、映射结论与未覆盖分支清单见
[`docs/specs/2026-10-14-conatus-core-go-design.md`](docs/specs/2026-10-14-conatus-core-go-design.md)；
开发约定见 [`AGENTS.md`](AGENTS.md)。

## 许可

MIT，见 [LICENSE](LICENSE)。
