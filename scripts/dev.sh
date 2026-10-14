#!/usr/bin/env bash
# 本地开发 / 演示环境：PostgreSQL 与 Redis（docker）、sub2api 替身、第三方服务替身（支付宝、微信支付、BazaarLink）、
# market-api、market-worker、market-edge，并写入演示数据。
# 替身不访问任何真实上游、不产生真实扣款（按真实协议签名与记账），只用于开发与演示，不能用于生产。
#
# 用法：
#   scripts/dev.sh up       启动全部服务（首次会建库并写入演示数据）
#   scripts/dev.sh down     停止服务
#   scripts/dev.sh reset    停止服务并删除开发数据库
#   scripts/dev.sh status   查看进程
#
# 前端开发：另开终端执行 cd web && pnpm dev，浏览器打开 http://127.0.0.1:5173（/api 代理到 :8080）。
# 也可以先 cd web && pnpm build，market-api 会直接提供 web/dist（http://127.0.0.1:8080）。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
SUB2API_REPO="${SUB2API_REPO:-$ROOT/../github-fork/sub2api}"
DATA="$ROOT/data/dev"
PG_DSN_BASE="postgres://tm:tm@127.0.0.1:55432"
MARKET_DB=tm_dev
FAKE_DB=tm_dev_sub2api
ADMIN_EMAIL=admin@dev.local
ADMIN_PASSWORD=admin-password-1
API=http://127.0.0.1:8080
THIRDPARTY=http://127.0.0.1:18082
KEYS="$DATA/pay-keys"
mkdir -p "$DATA"

ensure_containers() {
  docker start tm-test-pg >/dev/null 2>&1 || docker run -d --name tm-test-pg -e POSTGRES_USER=tm -e POSTGRES_PASSWORD=tm -e POSTGRES_DB=tm -p 55432:5432 postgres:16-alpine >/dev/null
  docker start tm-test-redis >/dev/null 2>&1 || docker run -d --name tm-test-redis -p 56379:6379 redis:7-alpine >/dev/null
  for _ in $(seq 1 30); do
    docker exec tm-test-pg pg_isready -U tm >/dev/null 2>&1 && return
    sleep 1
  done
  echo "PostgreSQL 未就绪" >&2
  exit 1
}

ensure_db() {
  local exists
  exists="$(docker exec tm-test-pg psql -U tm -d tm -Atc "SELECT 1 FROM pg_database WHERE datname = '$1'")"
  [[ "$exists" == "1" ]] || docker exec tm-test-pg psql -U tm -d tm -qc "CREATE DATABASE $1"
}

start() { # name, command...
  local name=$1
  shift
  if [[ -f "$DATA/$name.pid" ]] && kill -0 "$(cat "$DATA/$name.pid")" 2>/dev/null; then
    echo "  $name 已在运行"
    return
  fi
  nohup "$@" >"$DATA/$name.log" 2>&1 &
  echo $! >"$DATA/$name.pid"
  echo "  $name 已启动（日志 data/dev/$name.log）"
}

wait_http() { # 任何 HTTP 响应都算就绪
  for _ in $(seq 1 120); do
    curl -s -o /dev/null "$1" 2>/dev/null && return
    sleep 1
  done
  echo "等待 $1 超时" >&2
  exit 1
}

market_env() {
  export TM_ENV=dev
  export TM_DATABASE_DSN="$PG_DSN_BASE/$MARKET_DB?sslmode=disable"
  export TM_REDIS_ADDR=127.0.0.1:56379 TM_REDIS_DB=2
  export TM_SUB2API_BASE_URL=http://127.0.0.1:18081 TM_SUB2API_ADMIN_API_KEY=dev-admin-key
  export TM_SUB2API_REPLICA_DSN="$PG_DSN_BASE/$FAKE_DB?sslmode=disable"
  export TM_CRYPTO_ACTIVE_KEY_ID=k1 TM_CRYPTO_KEYS="k1:ZGV2LWtleS1kZXYta2V5LWRldi1rZXktZGV2LWtleS0="
  export TM_CRYPTO_HASH_PEPPER=dev-pepper
  export TM_AUTH_JWT_SECRET=dev-jwt-secret-dev-jwt-secret-dev-jwt-secret
  export TM_BOOTSTRAP_ADMIN_EMAIL=$ADMIN_EMAIL TM_BOOTSTRAP_ADMIN_PASSWORD=$ADMIN_PASSWORD
  export TM_HTTP_ADDR=127.0.0.1:8080 TM_EDGE_ADDR=127.0.0.1:8090 TM_EDGE_PUBLIC_URL=http://127.0.0.1:8090
  export TM_EDGE_SPOOL_DIR="$DATA/edge-spool"
  if [[ -f web/dist/index.html ]]; then export TM_HTTP_WEB_DIR="$ROOT/web/dist"; fi
}

api() { # method path [json]
  curl -fsS -X "$1" "$API$2" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' ${3:+-d "$3"}
}

seed() {
  [[ -f "$DATA/seeded" ]] && return
  echo "写入演示数据…"
  TOKEN="$(curl -fsS -X POST "$API/api/auth/login" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" | sed -E 's/.*"token":"([^"]+)".*/\1/')"
  # 等 worker 完成首次兼容自检，之前对 sub2api 的写入会被拒绝（安全模式）
  for _ in $(seq 1 60); do
    api GET /api/admin/sub2api/status | grep -q '"guard_state":"normal"' && break
    sleep 1
  done
  for p in anthropic openai gemini antigravity grok kimi zhipu minimax opencode_go; do
    api PUT "/api/admin/vendors/$p" '{"enabled":true}' >/dev/null
  done
  for i in $(seq 1 16); do
    api POST /api/admin/proxies "{\"name\":\"dev-proxy-$i\",\"protocol\":\"socks5\",\"host\":\"10.255.0.$i\",\"port\":1080,\"region\":\"dev\"}" >/dev/null
  done
  api POST /api/admin/pools '{"name":"tm-pool-claude","platform":"anthropic","sell_multiplier":0.8}' >/dev/null
  api POST /api/admin/pools '{"name":"tm-pool-gpt","platform":"openai","sell_multiplier":0.7}' >/dev/null
  api POST /api/admin/pools '{"name":"tm-pool-all","platform":"composite","sell_multiplier":0.9}' >/dev/null
  # 替身只会回答"口令题"：把内置题库下线，换成测智口令题，让上架检测能在本地通过。
  # 测智按题目判定至少需要 20 道可计分题，这里放 24 道。
  for id in $(api GET '/api/admin/probe-items?status=active' | grep -o '"id":[0-9]*' | cut -d: -f2); do
    api POST "/api/admin/probe-items/$id/retire" >/dev/null
  done
  for i in $(seq 1 24); do
    api POST /api/admin/probe-items "{\"suite\":\"inspection_capability\",\"check_key\":\"D07\",\"category\":\"recall\",\"prompt\":\"Q$i: reply with the code PONG$i and nothing else\",\"grader\":{\"type\":\"contains\",\"answer\":\"PONG$i\"},\"max_tokens\":32}" >/dev/null
  done
  api PUT /api/admin/settings '{"refund_lock_seconds":5,"price_change_cooldown_minutes":1}' >/dev/null
  touch "$DATA/seeded"
}

login_admin() {
  TOKEN="$(curl -fsS -X POST "$API/api/auth/login" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" | sed -E 's/.*"token":"([^"]+)".*/\1/')"
}

# 支付配置：通知地址指向本机控制面，支付宝与微信支付商户接入替身（请求体由 fake-thirdparty 生成）
seed_payments() {
  [[ -f "$DATA/seeded-payments" ]] && return
  login_admin
  api PUT /api/admin/settings "{\"payment_public_url\":\"$API\"}" >/dev/null
  api PUT /api/admin/payments/merchants/alipay "@$KEYS/merchant-alipay.json" >/dev/null
  api PUT /api/admin/payments/merchants/wechat "@$KEYS/merchant-wechat.json" >/dev/null
  touch "$DATA/seeded-payments"
}

# 第三方检测：BazaarLink 指向替身（Edge 是本机 http 地址，需要允许非 https 目标）。
# Claude 开启「身份与功能核验」流程并以第三方为来源，在上架检测、管理员检测、卖家复测与申诉复测中执行。
seed_thirdparty() {
  [[ -f "$DATA/seeded-thirdparty" ]] && return
  login_admin
  api PUT /api/admin/detection-providers/bazaarlink \
    "{\"enabled\":true,\"base_url\":\"$THIRDPARTY\",\"settings\":{\"allow_insecure_target\":true},\"max_runs_per_hour\":30,\"timeout_minutes\":30}" >/dev/null
  local all='["onboarding","scheduled","recovery","seller_manual","admin","adaptive","appeal"]'
  api PUT /api/admin/detection-policies/anthropic "{
    \"third_party\":[{\"provider\":\"bazaarlink\",\"triggers\":[\"onboarding\",\"admin\",\"seller_manual\",\"appeal\"]}],
    \"flows\":{
      \"intelligence\":{\"enabled\":true,\"scheduled_enabled\":false,\"interval_minutes\":360,\"triggers\":$all,
        \"full_triggers\":[\"onboarding\",\"appeal\",\"admin\"],\"models\":[{\"model\":\"claude-sonnet-4-5\",\"expected_pass_rate\":null}]},
      \"degradation\":{\"enabled\":true,\"scheduled_enabled\":false,\"interval_minutes\":360,\"triggers\":$all,
        \"full_triggers\":[\"onboarding\",\"appeal\",\"admin\"],\"models\":[{\"model\":\"claude-sonnet-4-5\",\"expected_pass_rate\":null}]},
      \"identity\":{\"enabled\":true,\"source\":\"third_party\",\"scheduled_enabled\":false,\"interval_minutes\":1440,
        \"triggers\":[\"onboarding\",\"admin\",\"seller_manual\",\"appeal\"],\"full_triggers\":$all}
    }}" >/dev/null
  touch "$DATA/seeded-thirdparty"
}

up() {
  [[ -d "$SUB2API_REPO/.git" ]] || { echo "找不到 sub2api 仓库：$SUB2API_REPO（用 SUB2API_REPO 指定）" >&2; exit 1; }
  ensure_containers
  ensure_db $MARKET_DB
  ensure_db $FAKE_DB
  echo "构建…"
  go build -o bin/ ./cmd/...
  echo "启动服务…"
  start fake-sub2api bin/fake-sub2api --dsn "$PG_DSN_BASE/$FAKE_DB?sslmode=disable" --sub2api-repo "$SUB2API_REPO" --listen 127.0.0.1:18081
  start fake-thirdparty bin/fake-thirdparty --keys-dir "$KEYS" --listen 127.0.0.1:18082
  wait_http "http://127.0.0.1:18081/api/v1/admin/system/version"
  wait_http "$THIRDPARTY/api/probe/run/none"
  market_env
  start api bin/api
  wait_http "$API/healthz"
  start worker bin/worker
  start edge bin/edge
  seed
  seed_payments
  seed_thirdparty
  cat <<INFO

开发环境已就绪：
  控制面 / 前端  $API        （前端开发服务器：cd web && pnpm dev → http://127.0.0.1:5173）
  买家调用地址   http://127.0.0.1:8090
  管理员         $ADMIN_EMAIL / $ADMIN_PASSWORD
  sub2api 替身   http://127.0.0.1:18081（Admin Key: dev-admin-key）
  第三方替身     ${THIRDPARTY}（支付宝收银台 / 扫码页、微信扫码页、BazaarLink 检测；二维码内容就是扫码页地址）
提示：worker 首次兼容自检通过后才会写入 sub2api；新上游的上架检测约 30 秒一轮，用量入账有约 2 分钟安全延迟。
INFO
}

down() {
  for name in edge worker api fake-thirdparty fake-sub2api; do
    if [[ -f "$DATA/$name.pid" ]]; then
      kill "$(cat "$DATA/$name.pid")" 2>/dev/null || true
      rm -f "$DATA/$name.pid"
      echo "  $name 已停止"
    fi
  done
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  reset)
    down
    docker exec tm-test-pg psql -U tm -d tm -qc "DROP DATABASE IF EXISTS $MARKET_DB WITH (FORCE)" -c "DROP DATABASE IF EXISTS $FAKE_DB WITH (FORCE)"
    docker exec tm-test-redis redis-cli -n 2 flushdb >/dev/null
    rm -rf "$DATA"
    echo "已重置"
    ;;
  status)
    for name in fake-sub2api fake-thirdparty api worker edge; do
      if [[ -f "$DATA/$name.pid" ]] && kill -0 "$(cat "$DATA/$name.pid")" 2>/dev/null; then echo "$name: 运行中"; else echo "$name: 未运行"; fi
    done
    ;;
  *) sed -n '2,13p' "$0"; exit 1 ;;
esac
