# conatus_go

conatus 的 **Go 实现**——「时空可组合性（Spatiotemporal Composability）」编程范式：
可逆效应（时间可组合性）+ 反应式共效应（空间可组合性）统一在一个 `Context` 上。

对应 Dart 原版：[github.com/Fi2zz/conatus](https://github.com/Fi2zz/conatus)
（本仓的语义、测试 oracle 与验证强度都对齐它的 `packages/conatus_core`）。

## 当前状态

**T0 spike**：只有 `core` 包（= Dart 的 `conatus_core`），零第三方依赖。

| 项 | 数量 |
|----|------|
| 实现 | 1011 行 / 8 文件 |
| 测试 | 2699 行 / 23 文件 / 124 个用例 |
| 验证层 | gofmt → vet → 单元 → 随机顺序(3 种子) → `-race` → 覆盖率 → 变异测试(7 个全杀) |

T1（foundation / llm / agent 等包）未开始，分档与估算见 `Go版本移植工作量调研.md`。

## 用法

```go
app := core.NewRoot("app")
stop, err := app.Provide("config", Config{Region: "cn-beijing"})
if err != nil {
    return err
}

app.Inject([]string{"config"}, func(child *core.Context) error {
    config, err := core.Require[Config](child, "config")
    if err != nil {
        return err
    }
    log.Println("activated:", config.Region)
    child.OnDispose(core.Ignore(func() { log.Println("deactivated") }))
    return nil
})

_ = stop()      // config 消失 → 依赖它的注入自动停用，子上下文的效应全部 LIFO 回滚
app.Dispose()
```

## 验证

```bash
go test ./... -count=1 -race          # 单元 + 竞态
bash tool/verify_reversibility.sh     # 六层一键验证（提交前必须通过）
bash tool/mutate_reversibility.sh     # 变异测试：7 个真 bug 必须全部被杀
```

`go` 若只装在 `/usr/local/go/bin` 未进 PATH，脚本会自己探测一次。

验证强度对齐 Dart 版：Theorem 16 的逐前缀还原 oracle、Theorem 7 的逆方程、三种假可逆的
负向对照、以及「测试是否真的在断言」的变异测试（含 2 个针对夹具本身的元检查）。
Go 侧另外加了 `-race`、goroutine 泄漏探针、真实 tick 停表与真文件句柄（`lsof`）计数。

## 与 Dart 版的形态差异（硬结论）

- 移除监听器用句柄：`reactor.Add(...)` 返回 `*ListenerHandle`（Go 函数值不可比较）。
- 失败走显式 `error` 返回；构造参数非法才 panic。
- 泛型服务查找是自由函数：`core.Get[T](ctx, key)` / `core.Require[T](ctx, key)`。
- 所有类型并发安全，且**绝不持锁调用外部代码**（回调里重入不会死锁）。

完整决策表、映射结论与未覆盖分支清单见
[`docs/specs/2026-10-14-conatus-core-go-design.md`](docs/specs/2026-10-14-conatus-core-go-design.md)；
开发约定见 [`AGENTS.md`](AGENTS.md)。

## 许可

MIT，见 [LICENSE](LICENSE)。
