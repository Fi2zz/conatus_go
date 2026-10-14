#!/usr/bin/env bash
#
# 全模块验证的一键入口：按顺序跑完所有层。
#
# 层与「能抓到什么」的对应：
#   gofmt      → 格式差异
#   go vet     → 类型与可疑构造
#   单元测试   → 行为回归
#   随机顺序   → 用例间隐式依赖（flaky 会让所有数字失效）
#   竞态检测   → Go 特有的数据竞争
#   行覆盖率   → 未被执行的机制分支（信息性；真正的闸门是变异测试）
#   变异测试   → 测试是否真的在断言
#
# fail-closed：任一层的工具缺失、崩溃或非零退出都计为未通过，绝不静默跳过。
#
# 用法：
#   bash tool/verify_all.sh            # 全模块
#   bash tool/verify_all.sh core       # 只跑 core 域（覆盖率只看 core/，变异只跑 core 条目）
#   bash tool/verify_all.sh foundation/tools
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
# shellcheck source=tool/lib.sh
source tool/lib.sh

resolve_go

SCOPE="${1:-}"
FAIL=0

layer_format
layer_vet
layer_test
layer_shuffle
layer_race
layer_coverage "$SCOPE"
layer_mutants "$SCOPE"

verdict
