#!/usr/bin/env bash
#
# 变异测试执行器：读 tool/mutants.txt，把真实 bug 逐个注入实现与验证夹具，确认测试
# 套件能杀掉每一个。用数据文件而不是把变异体写死在脚本里——T1 起有 10 个包。
#
# fail-closed 规则（任一条成立即硬失败，绝不算通过）：
#   * 模式在目标文件里出现次数 ≠ 1：改的不一定是想改的那一处
#   * 变异没改到文件（哈希前后一致）：变异体根本没跑
#   * 变异体编译不过：失败无法归因于某条测试
#   * 还原后与备份不是逐字节一致
#   * 任何变异体存活：验证有盲区
#
# 兼容 macOS 自带的 bash 3.2（不用数组、不用 associative array）。
#
# 用法：bash tool/mutate.sh [域前缀]
#   省略时跑全部条目；给了前缀（如 core / foundation/tools）只跑该前缀下的条目。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if ! command -v go >/dev/null 2>&1 && [ -x /usr/local/go/bin/go ]; then
  PATH="/usr/local/go/bin:$PATH"
  export PATH
fi
if ! command -v go >/dev/null 2>&1; then
  echo "go 不在 PATH 上：请先安装 Go 或把它加进 PATH" >&2
  exit 1
fi

MANIFEST="tool/mutants.txt"
[ -f "$MANIFEST" ] || { echo "缺少 $MANIFEST" >&2; exit 1; }

DOMAIN_FILTER="${1:-}"
BACKUP="$(mktemp -d)"
TARGET_FILES=""

backup_name() { echo "$BACKUP/$(echo "$1" | tr '/' '_')"; }
restore_file() { cp "$(backup_name "$1")" "$1"; }
restore_all() {
  local file
  while IFS= read -r file; do
    [ -n "$file" ] || continue
    restore_file "$file"
  done <<EOF
$TARGET_FILES
EOF
}
trap restore_all EXIT INT TERM

# 预先把所有目标文件备份一遍，EXIT 时无条件还原。
while IFS='|' read -r domain label file old new; do
  case "$domain" in ''|\#*) continue ;; esac
  case "$TARGET_FILES" in
    *"$file"$'\n'*) continue ;;
  esac
  [ -f "$file" ] || { echo "目标文件不存在：$file" >&2; exit 1; }
  cp "$file" "$(backup_name "$file")"
  TARGET_FILES="${TARGET_FILES}${file}"$'\n'
done < "$MANIFEST"

killed=0
survived=0
hard_failed=0
total=0

printf '%-22s %-30s %s\n' "域" "变异体" "结果"
printf '%-22s %-30s %s\n' "----------------------" "------------------------------" "----------------------------------"

while IFS='|' read -r domain label file old new; do
  case "$domain" in ''|\#*) continue ;; esac
  if [ -n "$DOMAIN_FILTER" ]; then
    case "$domain" in "$DOMAIN_FILTER"*) ;; *) continue ;; esac
  fi
  total=$((total + 1))

  case "$new" in
    *'|'*)
      printf '%-22s %-30s %s\n' "$domain" "$label" "硬失败：字段数不对（原文或替换里混进了分隔符 |）"
      hard_failed=$((hard_failed + 1))
      continue
      ;;
  esac

  occurrences="$(grep -F -c -- "$old" "$file")"
  if [ "$occurrences" != "1" ]; then
    printf '%-22s %-30s %s\n' "$domain" "$label" "硬失败：模式出现 $occurrences 次（应为 1）"
    hard_failed=$((hard_failed + 1))
    continue
  fi

  before="$(shasum -a 256 "$file" | cut -d' ' -f1)"
  OLD="$old" NEW="$new" perl -0pi -e 's/\Q$ENV{OLD}\E/$ENV{NEW}/' "$file"
  after="$(shasum -a 256 "$file" | cut -d' ' -f1)"

  if [ "$before" = "$after" ]; then
    printf '%-22s %-30s %s\n' "$domain" "$label" "硬失败：变异未生效"
    hard_failed=$((hard_failed + 1))
    restore_file "$file"
    continue
  fi

  if ! go vet ./... >/dev/null 2>&1; then
    printf '%-22s %-30s %s\n' "$domain" "$label" "硬失败：变异体编译不过"
    hard_failed=$((hard_failed + 1))
    restore_file "$file"
    continue
  fi

  output="$(go test ./... -count=1 2>&1)"
  status=$?
  restore_file "$file"

  if ! cmp -s "$(backup_name "$file")" "$file"; then
    printf '%-22s %-30s %s\n' "$domain" "$label" "硬失败：还原后与备份不一致"
    hard_failed=$((hard_failed + 1))
    continue
  fi

  if [ "$status" -ne 0 ]; then
    failures="$(printf '%s' "$output" | grep -c -E '^ *--- FAIL')"
    printf '%-22s %-30s %s\n' "$domain" "$label" "已杀死（$failures 条测试失败）"
    killed=$((killed + 1))
  else
    printf '%-22s %-30s %s\n' "$domain" "$label" "存活 → 验证存在盲区"
    survived=$((survived + 1))
  fi
done < "$MANIFEST"

printf '\n合计：杀死 %d，存活 %d，硬失败 %d（共 %d 个变异体）\n' \
  "$killed" "$survived" "$hard_failed" "$total"

if [ "$total" -eq 0 ]; then
  echo "结论：没有匹配的变异体（域前缀=${DOMAIN_FILTER:-全部}）→ 按 fail-closed 计为未通过"
  exit 1
fi
if [ "$survived" -gt 0 ] || [ "$hard_failed" -gt 0 ]; then
  echo "结论：未通过"
  exit 1
fi
echo "结论：全部变异体被杀死，检查器有效"
exit 0
