#!/usr/bin/env bash
# 本地开发 / 演示环境：PostgreSQL 与 Redis（docker）、真实 sub2api（默认）或替身、
# 第三方服务替身（支付宝、微信支付、BazaarLink）、market-api、market-worker、market-edge。
#
# 用法：
#   scripts/dev.sh up       启动全部服务（首次会建库并写入演示数据）
#   scripts/dev.sh down     停止服务
#   scripts/dev.sh reset    停止服务并删除开发数据库
#   scripts/dev.sh status   查看进程
#
# 默认 USE_REAL_ENGINE=1：起真实 backend/cmd/server（:18080，库 tm_sub2api_live）。
# 仅在需要旧替身时：USE_REAL_ENGINE=0 scripts/dev.sh up
# 浏览器只开 http://127.0.0.1:8080
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
# 宿主仓库根即完整 sub2api 树；过渡仓旁路可用 SUB2API_REPO 覆盖
SUB2API_REPO="${SUB2API_REPO:-$(cd "$ROOT/.." && pwd)}"
FRONTEND_DIR="${FRONTEND_DIR:-$SUB2API_REPO/frontend}"
BACKEND_DIR="${BACKEND_DIR:-$SUB2API_REPO/backend}"
DATA="${DEV_DATA_DIR:-$ROOT/data/dev}"
# 端口与容器名可覆盖：同一台机器上已有别的项目占用默认端口时，
# 用 DEV_PG_PORT / DEV_REDIS_PORT / DEV_PG_CONTAINER / DEV_REDIS_CONTAINER 起一套隔离环境。
PG_PORT="${DEV_PG_PORT:-55432}"
REDIS_PORT="${DEV_REDIS_PORT:-56379}"
PG_CONTAINER="${DEV_PG_CONTAINER:-tm-test-pg}"
REDIS_CONTAINER="${DEV_REDIS_CONTAINER:-tm-test-redis}"
PG_DSN_BASE="postgres://tm:tm@127.0.0.1:$PG_PORT"
MARKET_DB=tm_dev
FAKE_DB=tm_dev_sub2api
ENGINE_DB="${ENGINE_DB:-tm_sub2api_live}"
USE_REAL_ENGINE="${USE_REAL_ENGINE:-1}"
ADMIN_EMAIL=admin@dev.local
ADMIN_PASSWORD=admin-password-1
SELLER_EMAIL=seller@dev.local
SELLER_PASSWORD=seller-password-1
BUYER_EMAIL=buyer@dev.local
BUYER_PASSWORD=buyer-password-1
API=http://127.0.0.1:8080
ENGINE=http://127.0.0.1:18080
THIRDPARTY=http://127.0.0.1:18082
KEYS="$DATA/pay-keys"
mkdir -p "$DATA"

ensure_containers() {
  docker start "$PG_CONTAINER" >/dev/null 2>&1 || docker run -d --name "$PG_CONTAINER" -e POSTGRES_USER=tm -e POSTGRES_PASSWORD=tm -e POSTGRES_DB=tm -p "$PG_PORT":5432 postgres:16-alpine >/dev/null
  docker start "$REDIS_CONTAINER" >/dev/null 2>&1 || docker run -d --name "$REDIS_CONTAINER" -p "$REDIS_PORT":6379 redis:7-alpine >/dev/null
  for _ in $(seq 1 30); do
    docker exec "$PG_CONTAINER" pg_isready -U tm >/dev/null 2>&1 && return
    sleep 1
  done
  echo "PostgreSQL 未就绪" >&2
  exit 1
}

ensure_db() {
  local exists
  exists="$(docker exec "$PG_CONTAINER" psql -U tm -d tm -Atc "SELECT 1 FROM pg_database WHERE datname = '$1'")"
  [[ "$exists" == "1" ]] || docker exec "$PG_CONTAINER" psql -U tm -d tm -qc "CREATE DATABASE $1"
}

# 后台拉起进程；Linux 用 setsid 脱进程组，macOS 回退 nohup
launch_bg() {
  local log=$1
  shift
  if command -v setsid >/dev/null 2>&1; then
    setsid "$@" >"$log" 2>&1 </dev/null &
  else
    nohup "$@" >"$log" 2>&1 </dev/null &
  fi
  echo $!
}

start() { # name, command...
  local name=$1
  shift
  if [[ -f "$DATA/$name.pid" ]] && kill -0 "$(cat "$DATA/$name.pid")" 2>/dev/null; then
    echo "  $name 已在运行"
    return
  fi
  launch_bg "$DATA/$name.log" "$@" >"$DATA/$name.pid"
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

ensure_engine_admin_key() {
  local key_file="$DATA/engine-admin-key"
  if [[ -s "$key_file" ]]; then
    cat "$key_file"
    return
  fi
  local tok key
  tok="$(curl -fsS -X POST "$ENGINE/api/v1/auth/login" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["access_token"])')"
  curl -fsS -X POST "$ENGINE/api/v1/admin/compliance/accept" -H "Authorization: Bearer $tok" \
    -H 'Content-Type: application/json' \
    -d '{"phrase":"我已阅读、理解并同意 Sub2API 部署与运营合规承诺","language":"zh"}' >/dev/null || true
  key="$(curl -fsS -X POST "$ENGINE/api/v1/admin/settings/admin-api-key/regenerate" -H "Authorization: Bearer $tok" \
    | python3 -c 'import sys,json; d=json.load(sys.stdin); x=d.get("data") or d; print(x.get("key") or x.get("admin_api_key") or "")')"
  [[ -n "$key" ]] || { echo "无法生成引擎 Admin API Key" >&2; exit 1; }
  printf '%s' "$key" >"$key_file"
  echo "$key"
}

# 全新环境首次启动真实引擎会停在安装向导；带上 AUTO_SETUP 变量让它按本脚本的库/Redis/管理员自动安装。
# 已安装（DATA_DIR 下有 config.yaml / .installed）后这些变量不会被读取。
export_engine_auto_setup() {
  export AUTO_SETUP=true
  export DATABASE_HOST=127.0.0.1 DATABASE_PORT="$PG_PORT" DATABASE_USER=tm DATABASE_PASSWORD=tm DATABASE_DBNAME="$ENGINE_DB" DATABASE_SSLMODE=disable
  export REDIS_HOST=127.0.0.1 REDIS_PORT="$REDIS_PORT" REDIS_DB=3
  export ADMIN_EMAIL="$ADMIN_EMAIL" ADMIN_PASSWORD="$ADMIN_PASSWORD"
  export JWT_SECRET=dev-engine-jwt-secret-dev-engine-jwt-secret
}

start_real_engine() {
  ensure_db "$ENGINE_DB"
  echo "构建真实 sub2api…"
  (cd "$BACKEND_DIR" && go build -o "$ROOT/bin/sub2api" ./cmd/server)
  local sdata="$DATA/sub2api-data"
  mkdir -p "$sdata"
  if [[ -f "$sdata/server.pid" ]] && kill -0 "$(cat "$sdata/server.pid")" 2>/dev/null; then
    kill "$(cat "$sdata/server.pid")" 2>/dev/null || true
    sleep 0.4
  fi
  # enginehost.ServeAdmin 经 config.Load 读 TM_*；缺省则管理端 relay 全是 503
  local admin_key=""
  [[ -s "$DATA/engine-admin-key" ]] && admin_key="$(cat "$DATA/engine-admin-key")"
  (
    export DATA_DIR="$sdata" SERVER_HOST=127.0.0.1 SERVER_PORT=18080 SERVER_MODE=release TZ=Asia/Shanghai
    export_engine_auto_setup
    export MARKET_DATABASE_DSN="$PG_DSN_BASE/$MARKET_DB?sslmode=disable"
    export MARKET_ENGINE_DSN="$PG_DSN_BASE/$ENGINE_DB?sslmode=disable"
    export TM_ENV=dev
    export TM_DATABASE_DSN="$PG_DSN_BASE/$MARKET_DB?sslmode=disable"
    export TM_REDIS_ADDR=127.0.0.1:$REDIS_PORT TM_REDIS_DB=2
    export TM_CRYPTO_ACTIVE_KEY_ID=k1 TM_CRYPTO_KEYS="k1:ZGV2LWtleS1kZXYta2V5LWRldi1rZXktZGV2LWtleS0="
    export TM_CRYPTO_HASH_PEPPER=dev-pepper
    export TM_AUTH_JWT_SECRET=dev-jwt-secret-dev-jwt-secret-dev-jwt-secret
    export TM_BOOTSTRAP_ADMIN_EMAIL=$ADMIN_EMAIL TM_BOOTSTRAP_ADMIN_PASSWORD=$ADMIN_PASSWORD
    export TM_SUB2API_BASE_URL="$ENGINE"
    export TM_SUB2API_REPLICA_DSN="$PG_DSN_BASE/$ENGINE_DB?sslmode=disable"
    [[ -n "$admin_key" ]] && export TM_SUB2API_ADMIN_API_KEY="$admin_key"
    launch_bg "$sdata/server.log" "$ROOT/bin/sub2api" >"$sdata/server.pid"
  )
  wait_http "${ENGINE}/health"
  # 首次无 key 时：生成后带 TM_* 再启一次，保证 outbound / relay 可用
  if [[ -z "$admin_key" ]]; then
    admin_key="$(ensure_engine_admin_key)"
    kill "$(cat "$sdata/server.pid")" 2>/dev/null || true
    sleep 0.4
    (
      export DATA_DIR="$sdata" SERVER_HOST=127.0.0.1 SERVER_PORT=18080 SERVER_MODE=release TZ=Asia/Shanghai
      export_engine_auto_setup
      export MARKET_DATABASE_DSN="$PG_DSN_BASE/$MARKET_DB?sslmode=disable"
      export MARKET_ENGINE_DSN="$PG_DSN_BASE/$ENGINE_DB?sslmode=disable"
      export TM_ENV=dev
      export TM_DATABASE_DSN="$PG_DSN_BASE/$MARKET_DB?sslmode=disable"
      export TM_REDIS_ADDR=127.0.0.1:$REDIS_PORT TM_REDIS_DB=2
      export TM_CRYPTO_ACTIVE_KEY_ID=k1 TM_CRYPTO_KEYS="k1:ZGV2LWtleS1kZXYta2V5LWRldi1rZXktZGV2LWtleS0="
      export TM_CRYPTO_HASH_PEPPER=dev-pepper
      export TM_AUTH_JWT_SECRET=dev-jwt-secret-dev-jwt-secret-dev-jwt-secret
      export TM_BOOTSTRAP_ADMIN_EMAIL=$ADMIN_EMAIL TM_BOOTSTRAP_ADMIN_PASSWORD=$ADMIN_PASSWORD
      export TM_SUB2API_BASE_URL="$ENGINE"
      export TM_SUB2API_ADMIN_API_KEY="$admin_key"
      export TM_SUB2API_REPLICA_DSN="$PG_DSN_BASE/$ENGINE_DB?sslmode=disable"
      launch_bg "$sdata/server.log" "$ROOT/bin/sub2api" >"$sdata/server.pid"
    )
    wait_http "${ENGINE}/health"
  fi
  echo "  sub2api 真实引擎 已启动 (${ENGINE})"
}

market_env() {
  export TM_ENV=dev
  export TM_DATABASE_DSN="$PG_DSN_BASE/$MARKET_DB?sslmode=disable"
  export TM_REDIS_ADDR=127.0.0.1:$REDIS_PORT TM_REDIS_DB=2
  if [[ "$USE_REAL_ENGINE" == "1" ]]; then
    export TM_SUB2API_BASE_URL="$ENGINE"
    export TM_SUB2API_ADMIN_API_KEY="$(ensure_engine_admin_key)"
    export TM_SUB2API_REPLICA_DSN="$PG_DSN_BASE/$ENGINE_DB?sslmode=disable"
  else
    export TM_SUB2API_BASE_URL=http://127.0.0.1:18081 TM_SUB2API_ADMIN_API_KEY=dev-admin-key
    export TM_SUB2API_REPLICA_DSN="$PG_DSN_BASE/$FAKE_DB?sslmode=disable"
  fi
  export TM_CRYPTO_ACTIVE_KEY_ID=k1 TM_CRYPTO_KEYS="k1:ZGV2LWtleS1kZXYta2V5LWRldi1rZXktZGV2LWtleS0="
  export TM_CRYPTO_HASH_PEPPER=dev-pepper
  export TM_AUTH_JWT_SECRET=dev-jwt-secret-dev-jwt-secret-dev-jwt-secret
  export TM_BOOTSTRAP_ADMIN_EMAIL=$ADMIN_EMAIL TM_BOOTSTRAP_ADMIN_PASSWORD=$ADMIN_PASSWORD
  export TM_HTTP_ADDR=127.0.0.1:8080 TM_EDGE_ADDR=127.0.0.1:8090 TM_EDGE_PUBLIC_URL=http://127.0.0.1:8090
  export TM_EDGE_SPOOL_DIR="$DATA/edge-spool"
  if [[ -f web/dist/index.html ]]; then export TM_HTTP_WEB_DIR="$ROOT/web/dist"; fi
  # 管理端 SPA 挂 /admin（静态资源 /admin-assets）
  if [[ -f engine-dist/index.html ]]; then export TM_HTTP_ADMIN_WEB_DIR="$ROOT/engine-dist"; fi
}

build_frontends() {
  echo "构建前端（闲猫 SPA + 引擎 SPA）…"
  if [[ "${SKIP_FRONTEND_BUILD:-0}" == "1" ]]; then
    [[ -f web/dist/index.html ]] || { echo "SKIP_FRONTEND_BUILD=1 但缺少 web/dist" >&2; exit 1; }
    [[ -f engine-dist/index.html ]] || echo "  提示：缺少 engine-dist，管理端页可能不可用"
    echo "  跳过构建，复用已有 dist"
    return
  fi
  if [[ -d web/node_modules ]]; then
    if ! (cd web && node node_modules/vite/bin/vite.js build) >/dev/null; then
      [[ -f web/dist/index.html ]] || { echo "web 构建失败且无可用 dist" >&2; exit 1; }
      echo "  web 构建失败，复用已有 dist"
    fi
  elif [[ -f web/dist/index.html ]]; then
    echo "  复用已有 web/dist"
  else
    echo "缺少 market/web 依赖：请先 cd web && pnpm install" >&2
    exit 1
  fi
  if [[ -d "$FRONTEND_DIR/node_modules" ]]; then
    if ! (cd "$FRONTEND_DIR" && VITE_ENGINE_HOST=1 VITE_API_BASE_URL=/api/v1 node node_modules/vite/bin/vite.js build) >/dev/null; then
      [[ -f engine-dist/index.html ]] || echo "  引擎 SPA 构建失败且无 engine-dist"
      echo "  引擎 SPA 构建失败，复用已有 dist（若有）"
    fi
  elif [[ -f engine-dist/index.html ]]; then
    echo "  复用已有 engine-dist"
  else
    echo "缺少 frontend 依赖：请先 cd $FRONTEND_DIR && pnpm install（引擎页可选）" >&2
  fi
}

api() { # method path [json]
  if [[ $# -ge 3 ]]; then
    curl -fsS -X "$1" "$API$2" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$3"
  else
    curl -fsS -X "$1" "$API$2" -H "Authorization: Bearer $TOKEN"
  fi
}

# 幂等种子写入：201/200 成功；409 视为已存在；其它码重试
api_upsert() {
  local method=$1 path=$2 body=$3
  local i code
  for i in $(seq 1 30); do
    code="$(curl -sS -o /tmp/tm-dev-api.json -w '%{http_code}' -X "$method" "$API$path" \
      -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d "$body" || true)"
    case "$code" in
      200|201|204) return 0 ;;
      409) return 0 ;;
      503) sleep 1 ;;
      *) sleep 1 ;;
    esac
  done
  echo "api_upsert 失败 $method $path → HTTP ${code:-err}: $(head -c 200 /tmp/tm-dev-api.json 2>/dev/null)" >&2
  return 1
}

login_as() {
  TOKEN="$(curl -fsS -X POST "$API/api/auth/login" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$1\",\"password\":\"$2\"}" | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["access_token"])')"
}

seed() {
  [[ -f "$DATA/seeded" ]] && return
  echo "写入演示数据…"
  login_as "$ADMIN_EMAIL" "$ADMIN_PASSWORD"
  # 等 worker 完成首次兼容自检，之前对 sub2api 的写入会被拒绝（安全模式）
  for _ in $(seq 1 90); do
    if api GET /api/admin/sub2api/status | grep -q '"guard_state":"normal"'; then
      # status 切到 normal 与写通道真正放开之间可能还有一瞬间 503
      sleep 2
      break
    fi
    sleep 1
  done
  for p in anthropic openai gemini antigravity grok kimi zhipu minimax opencode_go; do
    api_upsert PUT "/api/admin/vendors/$p" '{"enabled":true}'
  done
  for i in $(seq 1 16); do
    api_upsert POST /api/admin/proxies "{\"name\":\"dev-proxy-$i\",\"protocol\":\"socks5\",\"host\":\"10.255.0.$i\",\"port\":1080,\"region\":\"dev\"}"
  done
  api_upsert POST /api/admin/pools '{"name":"tm-pool-claude","platform":"anthropic","sell_multiplier":0.8}'
  api_upsert POST /api/admin/pools '{"name":"tm-pool-gpt","platform":"openai","sell_multiplier":0.7}'
  api_upsert POST /api/admin/pools '{"name":"tm-pool-all","platform":"composite","sell_multiplier":0.9}'
  # 替身只会回答"口令题"：把内置题库下线，换成测智口令题，让上架检测能在本地通过。
  # 测智按题目判定至少需要 20 道可计分题，这里放 24 道。
  for id in $(api GET '/api/admin/probe-items?status=active' | grep -o '"id":[0-9]*' | cut -d: -f2); do
    api POST "/api/admin/probe-items/$id/retire" >/dev/null
  done
  for i in $(seq 1 24); do
    api POST /api/admin/probe-items "{\"suite\":\"inspection_capability\",\"check_key\":\"D07\",\"category\":\"recall\",\"prompt\":\"Q$i: reply with the code PONG$i and nothing else\",\"grader\":{\"type\":\"contains\",\"answer\":\"PONG$i\"},\"max_tokens\":32}" >/dev/null
  done
  api PUT /api/admin/settings '{"refund_lock_seconds":5,"price_change_cooldown_minutes":1,"require_seller_proxy":false}' >/dev/null
  touch "$DATA/seeded"
}

login_admin() {
  login_as "$ADMIN_EMAIL" "$ADMIN_PASSWORD"
}

# 注册买卖双方，并用替身 OAuth 接两条上游，供供给融合页联调。
seed_accounts() {
  [[ -f "$DATA/seeded-accounts" ]] && return
  echo "写入买卖双方与供给演示数据…"
  login_admin
  # 等平台代理同步到替身，否则创建上游会卡住
  for _ in $(seq 1 90); do
    synced="$(api GET /api/admin/proxies | python3 -c 'import sys,json; d=json.load(sys.stdin).get("data") or []; print(sum(1 for p in d if p.get("sub2api_proxy_id")))')"
    [[ "${synced:-0}" -ge 1 ]] && break
    sleep 1
  done
  curl -sS -o /dev/null -X POST "$API/api/auth/register" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$SELLER_EMAIL\",\"password\":\"$SELLER_PASSWORD\"}" || true
  curl -sS -o /dev/null -X POST "$API/api/auth/register" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$BUYER_EMAIL\",\"password\":\"$BUYER_PASSWORD\"}" || true
  login_as "$SELLER_EMAIL" "$SELLER_PASSWORD"
  local i=0
  for platform in anthropic openai; do
    i=$((i + 1))
    local body id start state code
    # 真实引擎下 OAuth 演示码可能不可用；失败不阻断环境启动
    body="$(curl -sS -X POST "$API/api/seller/upstreams" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
      -d "{\"platform\":\"$platform\",\"auth_type\":\"oauth\"}" || true)"
    id="$(printf '%s' "$body" | python3 -c 'import sys,json
try:
 d=json.load(sys.stdin); print((d.get("data") or {}).get("id") or "")
except Exception:
 print("")' 2>/dev/null || true)"
    [[ -n "$id" ]] || { echo "  跳过 $platform 上游演示接入（创建失败）"; continue; }
    start="$(curl -sS -X POST "$API/api/seller/upstreams/$id/oauth/start" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{}' || true)"
    state="$(printf '%s' "$start" | python3 -c 'import sys,json,urllib.parse
try:
 u=json.load(sys.stdin)["data"]["auth_url"]; print(urllib.parse.parse_qs(urllib.parse.urlparse(u).query).get("state",[""])[0])
except Exception:
 print("")' 2>/dev/null || true)"
    code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$API/api/seller/upstreams/$id/oauth/complete" \
      -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
      -d "{\"code\":\"https://cb.dev/callback?code=demo-$platform-$i&state=$state\"}" || true)"
    [[ "$code" == "200" || "$code" == "201" ]] || echo "  跳过 ${platform} OAuth 演示完成（HTTP ${code}）"
  done
  # 等出站把账号落到替身，供给融合页才能显示 linked
  login_admin
  for _ in $(seq 1 90); do
    linked="$(api GET /api/admin/supply/accounts | python3 -c 'import sys,json; d=json.load(sys.stdin).get("data") or []; print(sum(1 for r in d if r.get("linked")))')"
    [[ "${linked:-0}" -ge 1 ]] && break
    sleep 1
  done
  # 引擎 SPA 登录：替身库写入明文密码的管理员（与市场管理员同邮箱）。
  # users_email_unique_active 是部分唯一索引，不能用 ON CONFLICT (email)。
  docker exec "$PG_CONTAINER" psql -U tm -d "$FAKE_DB" -qc \
    "UPDATE users SET password_hash='$ADMIN_PASSWORD', role='admin', status='active', username='admin'
     WHERE email='$ADMIN_EMAIL' AND deleted_at IS NULL;
     INSERT INTO users (email, password_hash, username, notes, balance, concurrency, status, role)
     SELECT '$ADMIN_EMAIL', '$ADMIN_PASSWORD', 'admin', 'dev', 1000, 10, 'active', 'admin'
     WHERE NOT EXISTS (SELECT 1 FROM users WHERE email='$ADMIN_EMAIL' AND deleted_at IS NULL)" \
    >/dev/null 2>&1 || true
  touch "$DATA/seeded-accounts"
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
  build_frontends
  echo "构建后端…"
  go build -o bin/ ./cmd/...
  echo "启动服务…"
  if [[ "$USE_REAL_ENGINE" == "1" ]]; then
    start_real_engine
  else
    ensure_db $FAKE_DB
    start fake-sub2api bin/fake-sub2api --dsn "$PG_DSN_BASE/$FAKE_DB?sslmode=disable" --sub2api-repo "$SUB2API_REPO" --listen 127.0.0.1:18081
    wait_http "http://127.0.0.1:18081/api/v1/admin/system/version"
  fi
  start fake-thirdparty bin/fake-thirdparty --keys-dir "$KEYS" --listen 127.0.0.1:18082
  wait_http "$THIRDPARTY/api/probe/run/none"
  market_env
  start api bin/api
  wait_http "$API/healthz"
  start worker bin/worker
  start edge bin/edge
  seed
  seed_payments
  seed_thirdparty
  seed_accounts
  cat <<INFO

开发环境已就绪（单端口）：
  站点            $API
  C 端市场        $API/
  管理端          $API/admin/dashboard   （sub2api 底座 + 侧栏「闲猫」）
  买家调用 Edge   http://127.0.0.1:8090

  管理员（市场+引擎）  $ADMIN_EMAIL / $ADMIN_PASSWORD
  卖家                 $SELLER_EMAIL / $SELLER_PASSWORD
  买家                 $BUYER_EMAIL / $BUYER_PASSWORD

  sub2api 引擎  ${TM_SUB2API_BASE_URL:-$ENGINE} (USE_REAL_ENGINE=${USE_REAL_ENGINE:-1})
  第三方替身    ${THIRDPARTY}
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
  if [[ -f "$DATA/sub2api-data/server.pid" ]]; then
    kill "$(cat "$DATA/sub2api-data/server.pid")" 2>/dev/null || true
    rm -f "$DATA/sub2api-data/server.pid"
    echo "  sub2api 已停止"
  fi
}

case "${1:-}" in
  up) up ;;
  down) down ;;
  reset)
    down
    docker exec "$PG_CONTAINER" psql -U tm -d tm -qc "DROP DATABASE IF EXISTS $MARKET_DB WITH (FORCE)" -c "DROP DATABASE IF EXISTS $FAKE_DB WITH (FORCE)"
    docker exec "$REDIS_CONTAINER" redis-cli -n 2 flushdb >/dev/null
    rm -rf "$DATA"
    echo "已重置"
    ;;
  status)
    for name in fake-sub2api fake-thirdparty api worker edge; do
      if [[ -f "$DATA/$name.pid" ]] && kill -0 "$(cat "$DATA/$name.pid")" 2>/dev/null; then echo "$name: 运行中"; else echo "$name: 未运行"; fi
    done
    if [[ -f "$DATA/sub2api-data/server.pid" ]] && kill -0 "$(cat "$DATA/sub2api-data/server.pid")" 2>/dev/null; then
      echo "sub2api: 运行中"
    else
      echo "sub2api: 未运行"
    fi
    ;;
  *) sed -n '2,13p' "$0"; exit 1 ;;
esac
