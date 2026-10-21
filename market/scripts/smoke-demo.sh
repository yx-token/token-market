#!/usr/bin/env bash
set -euo pipefail
API="${API:-http://127.0.0.1:8080}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@dev.local}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-admin-password-1}"

fail=0
check() {
  local name=$1 want=$2
  shift 2
  code="$(curl -s -o /tmp/smoke.body -w '%{http_code}' "$@")"
  if [[ "$code" != "$want" ]]; then
    echo "FAIL $name (HTTP $code, want $want)"
    fail=1
  else
    echo "OK   $name"
  fi
}

check "healthz" 200 "$API/healthz"
check "c-end" 200 "$API/"
check "admin" 200 "$API/admin"
check "admin-dashboard" 200 "$API/admin/dashboard"
check "open-admin" 200 "$API/open-admin"
loc="$(curl -sI "$API/console" | tr -d '\r' | awk '/^Location:/{print $2}')"
if [[ "$loc" == "/admin/dashboard" ]]; then echo "OK   console→admin"; else echo "FAIL console→admin ($loc)"; fail=1; fi
loc="$(curl -sI "$API/engine" | tr -d '\r' | awk '/^Location:/{print $2}')"
if [[ "$loc" == "/admin/dashboard" ]]; then echo "OK   engine→admin"; else echo "FAIL engine→admin ($loc)"; fail=1; fi

TOKEN="$(curl -fsS -X POST "$API/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["access_token"])')"
check "vendors-api" 200 -H "Authorization: Bearer $TOKEN" "$API/api/admin/vendors"

title="$(curl -fsS "$API/admin/dashboard" | grep -o '<title>[^<]*</title>' | head -1)"
echo "OK   admin shell $title"
python3 - "$API" <<'PY'
import sys, urllib.request, re
api=sys.argv[1]
def js_paths(text):
    out=[]
    for m in re.findall(r'/?admin-assets/[^"\']+\.js', text):
        if not m.startswith('/'):
            m='/'+m
        out.append(m)
    return out
html=urllib.request.urlopen(api+"/admin/dashboard").read().decode()
seen=set(js_paths(html))
queue=list(seen)
blob=""
while queue:
    s=queue.pop()
    try:
        text=urllib.request.urlopen(api+s).read().decode("utf-8", "ignore")
    except Exception:
        continue
    blob += text
    for m in js_paths(text):
        if m not in seen:
            seen.add(m); queue.append(m)
need=["闲猫", "/admin/accounts", "/admin/dashboard", "/admin/market/overview"]
missing=[n for n in need if n not in blob]
if missing:
    print("FAIL admin nav missing", missing); sys.exit(2)
print("OK   admin nav is sub2api + 闲猫")
PY
bridge="$(curl -fsS -X POST "$API/api/admin/engine/session" -H "Authorization: Bearer $TOKEN" | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["user"]["role"])')"
if [[ "$bridge" == "admin" ]]; then echo "OK   sso bridge"; else echo "FAIL sso bridge ($bridge)"; fail=1; fi

check "admin-assets-page" 200 "$API/admin/accounts"
ENGINE_TOKEN="$(curl -fsS -X POST "$API/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" \
  | python3 -c 'import sys,json; d=json.load(sys.stdin)["data"]; assert d["user"]["role"]=="admin"; print(d["access_token"])')"
check "engine-accounts" 200 -H "Authorization: Bearer $ENGINE_TOKEN" "$API/api/v1/admin/accounts"
check "engine-groups" 200 -H "Authorization: Bearer $ENGINE_TOKEN" "$API/api/v1/admin/groups"
check "engine→闲猫 vendors" 200 -H "Authorization: Bearer $ENGINE_TOKEN" "$API/api/v1/admin/market/vendors"
check "engine→闲猫 status" 200 -H "Authorization: Bearer $ENGINE_TOKEN" "$API/api/v1/admin/market/status"
echo "OK   engine auth + 闲猫 APIs"
exit "$fail"
