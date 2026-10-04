#!/usr/bin/env bash
set -e
export PATH="$HOME/sdk/go/bin:$PATH" GOPATH="$HOME/go"
cd /workspace
go build -o bin/mealcheck ./cmd/server
for p in /proc/[0-9]*; do
  pid=${p##*/}
  [ "$pid" = "$$" ] && continue
  cmd=$(tr '\0' ' ' < "$p/cmdline" 2>/dev/null)
  case "$cmd" in
    *mealcheck*:18080*|*mealcheck*-addr\ :18080*|*/mealcheck\ -addr*)
      kill "$pid" 2>/dev/null || true ;;
  esac
done
# 兜底：按监听端口找
if command -v fuser >/dev/null 2>&1; then fuser -k 18080/tcp 2>/dev/null || true; fi
sleep 1
rm -f /tmp/e2e.db*
nohup bin/mealcheck -addr :18080 -db /tmp/e2e.db > /tmp/server.log 2>&1 &
sleep 2
python3 scripts/e2e_check.py
