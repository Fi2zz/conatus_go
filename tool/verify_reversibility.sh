#!/usr/bin/env bash
#
# 可逆效应验证的一键入口：按顺序跑完所有层。
#
# 层与「能抓到什么」的对应：
#   gofmt      → 格式差异（等价于 Dart 侧的 dart format 约束）
#   go vet     → 类型与可疑构造
#   单元测试   → 行为回归
#   随机顺序   → 用例间隐式依赖（flaky 会让所有数字失效）
#   竞态检测   → Go 特有的数据竞争（Dart 单线程模型下不存在这一层）
#   行覆盖率   → 未被执行的机制分支（信息性；真正的闸门是变异测试）
#   变异测试   → 测试是否真的在断言（本脚本的核心）
#
# fail-closed：任一层的工具缺失、崩溃或非零退出都计为未通过，绝不静默跳过。
#
# 用法：bash tool/verify_reversibility.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# go 可能只装在 /usr/local/go 而未进 PATH；探测一次，仍然找不到就直接失败。
if ! command -v go >/dev/null 2>&1 && [ -x /usr/local/go/bin/go ]; then
  PATH="/usr/local/go/bin:$PATH"
  export PATH
fi
if ! command -v go >/dev/null 2>&1; then
  echo "go 不在 PATH 上：请先安装 Go 或把它加进 PATH" >&2
  exit 1
fi

PKG="./core"
COVER="$(mktemp -d)"
fail=0

step() { printf '\n=== %s ===\n' "$1"; }
cleanup() { rm -rf "$COVER"; }
trap cleanup EXIT

step "格式检查（gofmt）"
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  echo "$unformatted"
  fail=1
else
  echo "  无差异"
fi

step "静态检查（go vet）"
go vet ./... || fail=1

step "单元测试"
go test ./... -count=1 || fail=1

step "随机顺序（3 个种子）"
for seed in 1 42 12345; do
  printf 'seed=%-6s ' "$seed"
  if go test ./... -count=1 -shuffle="$seed" >/dev/null 2>&1; then
    echo "通过"
  else
    echo "未通过"
    go test ./... -count=1 -shuffle="$seed" 2>&1 | tail -20
    fail=1
  fi
done

step "竞态检测（go test -race）"
go test ./... -count=1 -race || fail=1

step "行覆盖率（机制自身）"
if go test "$PKG" -count=1 -coverprofile="$COVER/cover.out" >/dev/null 2>&1 \
  && [ -s "$COVER/cover.out" ]; then
  awk 'NR > 1 {
         split($1, parts, ":")
         file = parts[1]
         total[file] += $2
         if ($3 + 0 > 0) covered[file] += $2
       }
       END {
         for (file in total) printf "  %s → %d / %d\n", file, covered[file] + 0, total[file] + 0
       }' "$COVER/cover.out" | sort
else
  echo "  跳过：覆盖率工具不可用 → 按 fail-closed 计为未通过"
  fail=1
fi

step "变异测试"
bash tool/mutate_reversibility.sh || fail=1

printf '\n'
if [ "$fail" -ne 0 ]; then
  echo "总判定：未通过"
  exit 1
fi
echo "总判定：通过"
