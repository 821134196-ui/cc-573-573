#!/usr/bin/env bash
# 园区团餐批次与收餐核对系统 —— 一条命令启动
# 用法: ./run.sh            构建（如需）并启动，监听 :8080
#       ./run.sh --dev      前端开发模式（热更新，:5173）+ 后端 :8080
#       PORT=:9090 ./run.sh 自定义监听地址
set -euo pipefail
cd "$(dirname "$0")"

ADDR="${ADDR:-:8080}"
DB_PATH="${DB_PATH:-data/mealcheck.db}"

# 定位 Go 工具链（本机无系统 Go 时使用 ~/sdk/go）
if ! command -v go >/dev/null 2>&1; then
  if [ -x "$HOME/sdk/go/bin/go" ]; then
    export PATH="$HOME/sdk/go/bin:$PATH"
  else
    echo "未找到 Go，请安装 Go 1.23+（或解压到 \$HOME/sdk/go）" >&2
    exit 1
  fi
fi
export GOPATH="${GOPATH:-$HOME/go}"
export GOTOOLCHAIN=local

# 1) 前端依赖与构建（嵌入二进制需要 web/dist）
if [ ! -d web/node_modules ]; then
  echo "==> 安装前端依赖"
  (cd web && npm install --no-audit --no-fund)
fi
if [ ! -f web/dist/index.html ]; then
  echo "==> 构建 Vue 前端"
  (cd web && npm run build)
fi

# 2) 构建后端
echo "==> 编译后端（Go + 嵌入前端）"
mkdir -p bin data
go build -o bin/mealcheck ./cmd/server

# 3) 启动
if [ "${1:-}" = "--dev" ]; then
  echo "==> 开发模式：后端 $ADDR，前端 :5173"
  (cd web && npm run dev) &
  VITE_PID=$!
  trap 'kill $VITE_PID 2>/dev/null || true' EXIT
  exec bin/mealcheck -addr "$ADDR" -db "$DB_PATH"
fi

echo "==> 启动系统：http://localhost${ADDR}  （数据 $DB_PATH）"
exec bin/mealcheck -addr "$ADDR" -db "$DB_PATH"
