#!/usr/bin/env bash
# 全量验证：Go 单测 + HTTP E2E（装箱签收核对 / 途中改派 / 差异复核 / 数量守恒 / 退款幂等）
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="${PATH}:$HOME/sdk/go/bin" GOPATH="${GOPATH:-$HOME/go}"

echo "==> go build"
go build -o bin/mealcheck ./cmd/server

echo "==> go test"
go test ./... -count=1 2>&1 | grep -v '^2026/'

# 停止旧实例（按进程名精确匹配，排除自身）
for p in /proc/[0-9]*; do
  pid=${p##*/}
  [ "$pid" = "$$" ] && continue
  if [ "$(cat "$p/comm" 2>/dev/null)" = "mealcheck" ]; then kill -9 "$pid" 2>/dev/null || true; fi
done
sleep 1
DB=/tmp/mealcheck-verify.db
rm -f "$DB"*
nohup bin/mealcheck -addr :18080 -db "$DB" > /tmp/mealcheck-verify.log 2>&1 &
SRV=$!
trap 'kill -9 $SRV 2>/dev/null || true' EXIT

for i in $(seq 1 30); do
  curl -sf http://localhost:18080/api/bootstrap >/dev/null 2>&1 && break
  sleep 0.5
done

echo "==> http e2e"
python3 scripts/e2e_check.py
