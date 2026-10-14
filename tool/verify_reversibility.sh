#!/usr/bin/env bash
#
# conatus_core 可逆性验证的专项入口：等价于 `bash tool/verify_all.sh core`
# （六层全跑，覆盖率只看 core/，变异测试只跑 core 域的条目）。
#
# 完整的层说明见 tool/lib.sh 与 tool/verify_all.sh。
#
# 用法：bash tool/verify_reversibility.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec bash "$ROOT/tool/verify_all.sh" core
