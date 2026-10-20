#!/usr/bin/env bash
# 全流程验收：C 端 → 管理端桥接 → 引擎页 → 闲猫市场 → 卖家/买家
set -euo pipefail
API="${API:-http://127.0.0.1:8080}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@dev.local}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin-password-1}"
SELLER_EMAIL="${SELLER_EMAIL:-seller@dev.local}"
SELLER_PASSWORD="${SELLER_PASSWORD:-seller-password-1}"
BUYER_EMAIL="${BUYER_EMAIL:-buyer@dev.local}"
BUYER_PASSWORD="${BUYER_PASSWORD:-buyer-password-1}"

fail=0
ok=0
pass() { echo "OK  $1"; ok=$((ok + 1)); }
bad() { echo "FAIL $1 — $2"; fail=$((fail + 1)); }

# usage: http_code URL [curl args...]
http_code() {
  local url=$1
  shift
  curl -s -o /tmp/flow.body -w '%{http_code}' "$@" -- "$url"
}

# usage: expect NAME WANT_CODE URL [curl args...]
expect() {
  local name=$1 want=$2 url=$3
  shift 3
  local c
  c=$(http_code "$url" "$@")
  if [[ "$c" == "$want" ]]; then pass "$name"
  else bad "$name" "HTTP $c $(head -c 120 /tmp/flow.body | tr '\n' ' ')"
  fi
}

echo "== 0 infra =="
expect "healthz" 200 "$API/healthz"
expect "c-end html" 200 "$API/"
title=$(curl -fsS "$API/" | grep -o '<title>[^<]*</title>' | head -1 || true)
[[ "$title" == *闲猫* ]] && pass "c-end title $title" || bad "c-end title" "${title:-empty}"

echo "== 1 admin entry =="
expect "admin shell" 200 "$API/admin"
expect "admin dashboard" 200 "$API/admin/dashboard"
loc=$(curl -sI "$API/console" | tr -d '\r' | awk '/^Location:/{print $2}')
[[ "$loc" == "/admin/dashboard" ]] && pass "console→admin" || bad "console→admin" "$loc"
loc=$(curl -sI "$API/engine" | tr -d '\r' | awk '/^Location:/{print $2}')
[[ "$loc" == "/admin/dashboard" ]] && pass "engine→admin" || bad "engine→admin" "$loc"
expect "open-admin page" 200 "$API/open-admin"

echo "== 2 market admin login =="
if curl -fsS -X POST "$API/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" >/tmp/mlogin.json; then
  pass "market admin login"
else
  bad "market admin login" "$(head -c 160 /tmp/mlogin.json 2>/dev/null || true)"
fi
MT=$(python3 -c 'import json; print(json.load(open("/tmp/mlogin.json"))["data"]["access_token"])')
if curl -fsS -H "Authorization: Bearer $MT" "$API/api/auth/me" >/tmp/mme.json \
  && python3 -c 'import json; u=json.load(open("/tmp/mme.json"))["data"]; assert u.get("is_admin") or u.get("role")=="admin"'; then
  pass "market /auth/me admin"
else
  bad "market /auth/me" "$(head -c 160 /tmp/mme.json 2>/dev/null || true)"
fi

echo "== 3 market → engine SSO =="
if curl -fsS -X POST "$API/api/admin/engine/session" -H "Authorization: Bearer $MT" >/tmp/esess.json; then
  pass "engine session bridge"
else
  bad "engine session bridge" "$(head -c 160 /tmp/esess.json 2>/dev/null || true)"
fi
ET=$(python3 -c 'import json; d=json.load(open("/tmp/esess.json"))["data"]; assert d["user"]["role"]=="admin"; print(d["access_token"])')
if curl -fsS -H "Authorization: Bearer $ET" "$API/api/v1/auth/me" >/tmp/eme.json \
  && python3 -c 'import json; assert json.load(open("/tmp/eme.json"))["data"]["role"]=="admin"'; then
  pass "engine /auth/me"
else
  bad "engine /auth/me" "$(head -c 160 /tmp/eme.json 2>/dev/null || true)"
fi

echo "== 4 engine → market SSO =="
if curl -fsS -X POST "$API/api/auth/from-engine" -H "Authorization: Bearer $ET" >/tmp/frome.json; then
  pass "from-engine bridge"
else
  bad "from-engine bridge" "$(head -c 160 /tmp/frome.json 2>/dev/null || true)"
fi
MT2=$(python3 -c 'import json; print(json.load(open("/tmp/frome.json"))["data"]["access_token"])')
expect "market api with from-engine token" 200 "$API/api/admin/vendors" -H "Authorization: Bearer $MT2"

echo "== 5 engine admin APIs =="
for p in \
  /api/v1/admin/dashboard/stats \
  /api/v1/admin/dashboard/realtime \
  /api/v1/admin/dashboard/trend \
  /api/v1/admin/accounts \
  /api/v1/admin/groups \
  /api/v1/admin/users \
  /api/v1/admin/proxies \
  /api/v1/admin/settings \
  /api/v1/admin/compliance \
  /api/v1/settings/public
do
  expect "engine $p" 200 "$API$p" -H "Authorization: Bearer $ET"
done
curl -fsS -H "Authorization: Bearer $ET" "$API/api/v1/admin/dashboard/stats" >/tmp/stats.json
acct=$(python3 -c 'import json; d=json.load(open("/tmp/stats.json"))["data"]; n=d.get("total_accounts", d.get("normal_accounts")); assert n is not None; print(n)') \
  && pass "dashboard stats shape (accounts=$acct)" \
  || bad "dashboard stats shape" "$(head -c 120 /tmp/stats.json)"

echo "== 6 闲猫市场 admin APIs =="
for p in \
  /api/admin/sub2api/status \
  /api/admin/supply/accounts \
  /api/admin/supply/groups \
  /api/admin/vendors \
  /api/admin/pools \
  /api/admin/proxies \
  /api/admin/upstreams \
  /api/admin/market-categories \
  /api/admin/market-discovery \
  /api/admin/recharges \
  /api/admin/refunds \
  /api/admin/withdrawals \
  /api/admin/appeals \
  /api/admin/quality/overview \
  /api/admin/inspection/checks \
  /api/admin/detection-policies \
  /api/admin/detection-providers \
  /api/admin/probe-items \
  /api/admin/probe-runs \
  /api/admin/payments/status \
  /api/admin/payments/merchants \
  /api/admin/settings
do
  expect "market $p" 200 "$API$p" -H "Authorization: Bearer $MT"
done
curl -fsS -H "Authorization: Bearer $MT" "$API/api/admin/supply/accounts" >/tmp/supply.json
if python3 -c 'import json; d=json.load(open("/tmp/supply.json")).get("data") or []; assert len(d)>=1; print(len(d), sum(1 for r in d if r.get("linked")))'; then
  pass "supply fusion non-empty"
else
  bad "supply fusion" "empty or invalid"
fi

echo "== 7 seller =="
if curl -fsS -X POST "$API/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$SELLER_EMAIL\",\"password\":\"$SELLER_PASSWORD\"}" >/tmp/slogin.json; then
  pass "seller login"
else
  bad "seller login" "$(head -c 120 /tmp/slogin.json 2>/dev/null || true)"
fi
ST=$(python3 -c 'import json; print(json.load(open("/tmp/slogin.json"))["data"]["access_token"])')
for p in /api/seller/upstreams /api/seller/proxies /api/seller/listings /api/seller/earnings; do
  expect "seller $p" 200 "$API$p" -H "Authorization: Bearer $ST"
done

echo "== 8 buyer + public market =="
if curl -fsS -X POST "$API/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$BUYER_EMAIL\",\"password\":\"$BUYER_PASSWORD\"}" >/tmp/blogin.json; then
  pass "buyer login"
else
  bad "buyer login" "$(head -c 120 /tmp/blogin.json 2>/dev/null || true)"
fi
BT=$(python3 -c 'import json; print(json.load(open("/tmp/blogin.json"))["data"]["access_token"])')
for p in /api/buyer/wallet /api/buyer/keys /api/buyer/usage /api/buyer/bills; do
  expect "buyer $p" 200 "$API$p" -H "Authorization: Bearer $BT"
done
for p in /api/market/listings /api/market/vendors /api/market/categories /api/market/discovery; do
  expect "public $p" 200 "$API$p"
done

echo "== 9 SPA assets + brand =="
ENGINE_DIST="${ENGINE_DIST:-$(cd "$(dirname "$0")/.." && pwd)/engine-dist}"
python3 - "$API" "$ENGINE_DIST" <<'PY'
import pathlib, re, sys, urllib.request
api, dist_root = sys.argv[1], pathlib.Path(sys.argv[2])
html = urllib.request.urlopen(api + "/admin/dashboard").read().decode()
srcs = re.findall(r'/admin-assets/[^"\']+\.js', html)
if not srcs:
    print("FAIL no engine assets")
    sys.exit(2)
if urllib.request.urlopen(api + srcs[0]).getcode() != 200:
    print("FAIL asset", srcs[0])
    sys.exit(2)
blob = ""
for s in srcs:
    blob += urllib.request.urlopen(api + s).read().decode("utf-8", "ignore")
# 闲猫市场 chunk 懒加载，扫全部构建产物
for p in (dist_root / "admin-assets").glob("*.js"):
    blob += p.read_text(errors="ignore")
need = ["闲猫", "/admin/accounts", "/admin/dashboard", "/admin/market/overview", "engine_auth_token", "/api/v1/admin/market"]
miss = [n for n in need if n not in blob]
if miss:
    print("FAIL bundle missing", miss)
    sys.exit(2)
print("OK assets+brand", srcs[0])
PY
[[ $? -eq 0 ]] && pass "engine assets + 闲猫 brand/nav" || bad "engine assets/brand" "see above"

echo "== 10 sub2api session on 闲猫 APIs (/api/v1/admin/market) =="
expect "engine token → market vendors" 200 "$API/api/v1/admin/market/vendors" -H "Authorization: Bearer $ET"
expect "engine token → market status" 200 "$API/api/v1/admin/market/status" -H "Authorization: Bearer $ET"
expect "engine token → market supply" 200 "$API/api/v1/admin/market/supply/accounts" -H "Authorization: Bearer $ET"
expect "engine token → identities" 200 "$API/api/v1/admin/market/identities" -H "Authorization: Bearer $ET"

echo "== 11 host frontend shop + login link =="
expect "shop spa" 200 "$API/shop"
expect "shop listing spa" 200 "$API/shop/listings/1"
expect "shop center spa" 200 "$API/shop/center"
python3 - <<'PY'
import json
s=json.load(open("/tmp/slogin.json"))["data"]
b=json.load(open("/tmp/blogin.json"))["data"]
assert s.get("engine_user_id"), s
assert s.get("engine_token_ready") is True, s
assert b.get("engine_user_id"), b
assert b.get("engine_token_ready") is True, b
print("OK seller/buyer engine link", s["engine_user_id"], b["engine_user_id"])
PY
[[ $? -eq 0 ]] && pass "seller/buyer engine_user_id" || bad "engine link" "login payload missing engine_user_id"
expect "buyer merchants" 200 "$API/api/buyer/merchants" -H "Authorization: Bearer $BT"

echo
echo "==== SUMMARY: $ok passed, $fail failed ===="
exit "$fail"
