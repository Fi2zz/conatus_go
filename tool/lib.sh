#!/usr/bin/env bash
#
# 验证脚本共用的层实现。被 tool/verify_all.sh 与 tool/verify_reversibility.sh 引用。
#
# 约定：调用方先 `set -uo pipefail` 并设 `FAIL=0`，每层调用后由本库维护 FAIL。
# fail-closed：工具缺失、崩溃或非零退出一律计为未通过，绝不静默跳过。

step() { printf '\n=== %s ===\n' "$1"; }

# go 可能只装在 /usr/local/go 而未进 PATH；探测一次，仍然找不到就直接失败。
resolve_go() {
  if ! command -v go >/dev/null 2>&1 && [ -x /usr/local/go/bin/go ]; then
    PATH="/usr/local/go/bin:$PATH"
    export PATH
  fi
  if ! command -v go >/dev/null 2>&1; then
    echo "go 不在 PATH 上：请先安装 Go 或把它加进 PATH" >&2
    exit 1
  fi
}

layer_format() {
  step "格式检查（gofmt）"
  local unformatted
  unformatted="$(gofmt -l .)"
  if [ -n "$unformatted" ]; then
    echo "$unformatted"
    FAIL=1
  else
    echo "  无差异"
  fi
}

layer_vet() {
  step "静态检查（go vet）"
  go vet ./... || FAIL=1
}

layer_test() {
  step "单元测试"
  go test ./... -count=1 || FAIL=1
}

layer_shuffle() {
  step "随机顺序（3 个种子）"
  local seed
  for seed in 1 42 12345; do
    printf 'seed=%-6s ' "$seed"
    if go test ./... -count=1 -shuffle="$seed" >/dev/null 2>&1; then
      echo "通过"
    else
      echo "未通过"
      go test ./... -count=1 -shuffle="$seed" 2>&1 | tail -20
      FAIL=1
    fi
  done
}

layer_race() {
  step "竞态检测（go test -race）"
  go test ./... -count=1 -race || FAIL=1
}

# 行覆盖率（信息性输出：真正的闸门是变异测试）。
# 传入路径前缀时只打印该前缀下的文件。
layer_coverage() {
  step "行覆盖率"
  local prefix="${1:-}"
  local cover
  cover="$(mktemp -d)/cover.out"
  if go test ./... -count=1 -coverprofile="$cover" >/dev/null 2>&1 && [ -s "$cover" ]; then
    awk -v prefix="$prefix" '
      NR > 1 {
        split($1, parts, ":")
        file = parts[1]
        if (prefix != "" && index(file, prefix) == 0) next
        total[file] += $2
        if ($3 + 0 > 0) covered[file] += $2
      }
      END {
        for (file in total) printf "  %s → %d / %d\n", file, covered[file] + 0, total[file] + 0
      }' "$cover" | sort
    rm -f "$cover"
  else
    echo "  跳过：覆盖率工具不可用 → 按 fail-closed 计为未通过"
    FAIL=1
  fi
}

layer_mutants() {
  step "变异测试"
  bash tool/mutate.sh "${1:-}" || FAIL=1
}

verdict() {
  printf '\n'
  if [ "$FAIL" -ne 0 ]; then
    echo "总判定：未通过"
    exit 1
  fi
  echo "总判定：通过"
}
