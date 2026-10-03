#!/usr/bin/env bash
# 一条命令启动：构建前端（如需）→ 启动 Go 服务（同时托管 API 与前端页面）
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 兼容 Go 安装在用户目录的情况
if [ -x "$HOME/.local/go/bin/go" ]; then
  export PATH="$HOME/.local/go/bin:$PATH"
fi

if [ ! -f "$ROOT/backend/frontend_dist/index.html" ]; then
  echo "==> 首次启动：构建 Vue 前端"
  cd "$ROOT/frontend"
  [ -d node_modules ] || npm install --no-audit --no-fund
  npm run build
fi

cd "$ROOT/backend"
export DB_DSN="${DB_DSN:-$ROOT/backend/campusmeal.db}"
export ADDR="${ADDR:-:8080}"
echo "==> 服务启动中：http://localhost${ADDR}"
echo "    重置演示数据：rm $ROOT/backend/campusmeal.db 后重新启动"
exec go run .
