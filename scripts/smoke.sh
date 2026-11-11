#!/usr/bin/env bash
# End-to-end smoke test against the public gateway.
# Usage: ./scripts/smoke.sh   (requires curl and python3)
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -f "$ROOT_DIR/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  . "$ROOT_DIR/.env"
  set +a
fi

BASE_URL="${BASE_URL:-http://localhost:8080}"
ADMIN_EMAIL="${BOOTSTRAP_ADMIN_EMAIL:-admin@fayrly.local}"
ADMIN_PASSWORD="${BOOTSTRAP_ADMIN_PASSWORD:-FayrlyAdmin123!}"
CORS_ORIGIN="${CORS_ORIGIN:-http://localhost:3000}"
RUN_ID="${RUN_ID:-$(date +%s)}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

PASS=0
FAIL=0
STATUS=""

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }
ok() { PASS=$((PASS + 1)); printf '  \033[32mPASS\033[0m %s\n' "$*"; }
no() { FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$*"; }

call() {
  local method=$1 path=$2 token=${3:-} data=${4:-}
  local args=(-sS --max-time 20 -o "$TMP_DIR/body.json" -w '%{http_code}' -X "$method" "$BASE_URL$path")
  if [[ -n "$token" ]]; then args+=(-H "Authorization: Bearer $token"); fi
  if [[ -n "$data" ]]; then args+=(-H 'Content-Type: application/json' --data-binary "$data"); fi
  STATUS=$(curl "${args[@]}") || STATUS="000"
}

expect_status() {
  local label=$1 expected=$2
  if [[ "$STATUS" == "$expected" ]]; then
    ok "$label (HTTP $STATUS)"
  else
    no "$label: expected HTTP $expected, got $STATUS"
    printf '       body: %s\n' "$(head -c 400 "$TMP_DIR/body.json" 2>/dev/null || true)"
  fi
}

field() {
  python3 - "$TMP_DIR/body.json" "$1" <<'PY'
import json, sys
value = json.load(open(sys.argv[1]))
for part in sys.argv[2].split("."):
    value = value[int(part)] if part.isdigit() else value[part]
if isinstance(value, bool):
    print("true" if value else "false")
elif isinstance(value, (dict, list)):
    print(json.dumps(value))
else:
    print(value)
PY
}

check() {
  local label=$1 expr=$2
  if python3 - "$TMP_DIR/body.json" "$expr" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
sys.exit(0 if eval(sys.argv[2]) else 1)
PY
  then
    ok "$label"
  else
    no "$label"
    printf '       body: %s\n' "$(head -c 400 "$TMP_DIR/body.json" 2>/dev/null || true)"
  fi
}

say "Waiting for the gateway"
READY=0
for _ in $(seq 1 60); do
  code=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 3 "$BASE_URL/readyz" || true)
  if [[ "$code" == "200" ]]; then READY=1; break; fi
  sleep 2
done
if [[ "$READY" != "1" ]]; then
  echo "Gateway is not ready at $BASE_URL/readyz" >&2
  curl -sS "$BASE_URL/readyz" || true
  exit 1
fi
ok "gateway and every upstream report ready"

say "Health endpoints"
call GET /healthz
expect_status "GET /healthz" 200
call GET /readyz
expect_status "GET /readyz" 200
check "all services ok" 'd["status"] == "ok" and all(v == "ok" for v in d["services"].values())'

say "Authentication"
call POST /api/v1/auth/login "" "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}"
expect_status "superadmin login" 200
ADMIN_TOKEN=$(field access_token)
ADMIN_REFRESH=$(field refresh_token)
ADMIN_ID=$(field user.id)

call GET /api/v1/auth/me "$ADMIN_TOKEN"
expect_status "GET /me with access token" 200
check "profile is the bootstrap superadmin" "d['user']['email'].lower() == '$ADMIN_EMAIL'.lower() and 'superadmin' in d['user']['roles']"

call POST /api/v1/auth/firebase "" '{"id_token":"not-a-real-token"}'
if [[ "$STATUS" == "503" || "$STATUS" == "401" ]]; then
  ok "firebase endpoint rejects an unverified token (HTTP $STATUS)"
else
  no "firebase endpoint: expected 503 (unconfigured) or 401, got $STATUS"
fi

call POST /api/v1/auth/register "" "{\"email\":\"member.$RUN_ID@example.com\",\"password\":\"MemberPass123!\",\"name\":\"Smoke Member\"}"
expect_status "register a member" 201
MEMBER_TOKEN=$(field access_token)

say "RBAC and access matrix"
call GET /api/v1/access-matrix "$ADMIN_TOKEN"
expect_status "GET /access-matrix" 200
check "superadmin holds every permission" "next(r for r in d['roles'] if r['key'] == 'superadmin')['permissions'] and set(next(r for r in d['roles'] if r['key'] == 'superadmin')['permissions']) == {p['key'] for p in d['permissions']}"
check "permission catalogue is seeded" 'len(d["permissions"]) >= 13'

call GET /api/v1/permissions "$ADMIN_TOKEN"
expect_status "GET /permissions" 200

call GET /api/v1/users "$MEMBER_TOKEN"
expect_status "member cannot list users" 403

call POST /api/v1/events "$MEMBER_TOKEN" '{"title":"Unauthorized Event","type":"concert","starts_at":"2027-01-01T10:00:00Z"}'
expect_status "member cannot create events" 403

call POST /api/v1/roles "$ADMIN_TOKEN" "{\"key\":\"smoke_$RUN_ID\",\"name\":\"Smoke Reviewer $RUN_ID\",\"description\":\"Temporary role created by smoke.sh\",\"permissions\":[\"report:read\",\"report:approve\"]}"
expect_status "create a role with permissions" 201
SMOKE_ROLE_ID=$(field data.id)

call PUT "/api/v1/roles/$SMOKE_ROLE_ID/permissions" "$ADMIN_TOKEN" '{"permissions":["report:read","report:approve"]}'
expect_status "PUT role permissions" 200
check "role has the requested permissions" 'sorted(d["data"]["permissions"]) == ["report:approve","report:read"]'

call PUT /api/v1/access-matrix "$ADMIN_TOKEN" "{\"roles\":[{\"role_id\":\"$SMOKE_ROLE_ID\",\"permissions\":[\"report:read\",\"report:approve\"]}]}"
expect_status "PUT access matrix for one role" 200

call POST /api/v1/users "$ADMIN_TOKEN" "{\"email\":\"reviewer.$RUN_ID@example.com\",\"name\":\"Smoke Reviewer\",\"password\":\"ReviewerPass123!\",\"roles\":[\"smoke_$RUN_ID\"]}"
expect_status "superadmin creates a reviewer user" 201
REVIEWER_ID=$(field data.id)

call POST /api/v1/auth/login "" "{\"email\":\"reviewer.$RUN_ID@example.com\",\"password\":\"ReviewerPass123!\"}"
expect_status "reviewer login" 200
REVIEWER_TOKEN=$(field access_token)
REVIEWER_REFRESH=$(field refresh_token)

call GET /api/v1/reports "$REVIEWER_TOKEN"
expect_status "reviewer can list reports" 200

call POST /api/v1/reports "$REVIEWER_TOKEN" '{"title":"Should Not Be Allowed","type":"other"}'
expect_status "reviewer cannot create reports (no report:write)" 403

say "Event lifecycle"
call POST /api/v1/events "$ADMIN_TOKEN" "{\"title\":\"Smoke Charity Gala $RUN_ID\",\"type\":\"charity\",\"description\":\"Created by scripts/smoke.sh\",\"venue_name\":\"Grand Hall\",\"city\":\"Jakarta\",\"country\":\"ID\",\"starts_at\":\"2027-06-01T19:00:00Z\",\"ends_at\":\"2027-06-01T23:00:00Z\",\"capacity\":500,\"price_min\":100000,\"price_max\":500000,\"currency\":\"IDR\",\"beneficiary\":\"Yayasan Harapan\",\"donation_target\":50000000,\"tags\":[\"smoke\",\"charity\"]}"
expect_status "create a charity event" 201
EVENT_ID=$(field data.id)
EVENT_SLUG=$(field data.slug)
check "new event starts as draft" 'd["data"]["status"] == "draft"'

call GET "/api/v1/events/$EVENT_ID"
expect_status "draft is hidden from anonymous callers" 404

call POST "/api/v1/events/$EVENT_ID/status" "$ADMIN_TOKEN" '{"status":"published"}'
expect_status "publish the event" 200
check "event is published" 'd["data"]["status"] == "published"'

call GET "/api/v1/events/$EVENT_ID"
expect_status "anonymous callers can read a published event" 200
check "public detail returns the same slug" "d['data']['slug'] == '$EVENT_SLUG'"

call GET "/api/v1/events?q=Smoke%20Charity%20Gala%20$RUN_ID"
expect_status "anonymous event search" 200
check "published event appears in the public list" "any(e['id'] == '$EVENT_ID' for e in d['data']) and len(d['data']) == 1"

call PATCH "/api/v1/events/$EVENT_ID" "$ADMIN_TOKEN" '{"venue_name":"Grand Hall Senayan"}'
expect_status "update the event" 200
check "venue was updated" 'd["data"]["venue_name"] == "Grand Hall Senayan"'

call POST "/api/v1/events/$EVENT_ID/status" "$ADMIN_TOKEN" '{"status":"completed"}'
expect_status "published cannot jump straight to completed" 409

call POST "/api/v1/events/$EVENT_ID/status" "$ADMIN_TOKEN" '{"status":"ongoing"}'
expect_status "move event to ongoing" 200

call POST "/api/v1/events/$EVENT_ID/status" "$ADMIN_TOKEN" '{"status":"completed"}'
expect_status "move event to completed" 200

call PATCH "/api/v1/events/$EVENT_ID" "$ADMIN_TOKEN" '{"title":"Late Edit"}'
expect_status "completed events are locked for edits" 409

say "Report workflow"
call POST /api/v1/reports "$ADMIN_TOKEN" "{\"title\":\"Gala Financial Report $RUN_ID\",\"type\":\"financial\",\"event_id\":\"$EVENT_ID\",\"event_title\":\"Smoke Charity Gala $RUN_ID\",\"period_start\":\"2026-01-01\",\"period_end\":\"2026-01-31\",\"summary\":\"Revenue and expenses for the gala.\",\"metrics\":{\"revenue\":150000000,\"expenses\":90000000}}"
expect_status "create a financial report" 201
REPORT_ID=$(field data.id)

call POST "/api/v1/reports/$REPORT_ID/submit" "$ADMIN_TOKEN"
expect_status "submit the report" 200
check "report is submitted" 'd["data"]["status"] == "submitted"'

call POST "/api/v1/reports/$REPORT_ID/review" "$ADMIN_TOKEN" '{"decision":"approved"}'
expect_status "author cannot review their own report" 403

call POST "/api/v1/reports/$REPORT_ID/review" "$REVIEWER_TOKEN" '{"decision":"approved","notes":"Numbers verified."}'
expect_status "reviewer approves the report" 200
check "report is approved" 'd["data"]["status"] == "approved" and d["data"]["reviewed_by"]'

call POST "/api/v1/reports/$REPORT_ID/review" "$REVIEWER_TOKEN" '{"decision":"approved"}'
expect_status "approved reports cannot be reviewed again" 409

call DELETE "/api/v1/reports/$REPORT_ID" "$ADMIN_TOKEN"
expect_status "approved reports cannot be deleted" 409

call POST /api/v1/reports "$ADMIN_TOKEN" "{\"title\":\"Rejected Report $RUN_ID\",\"type\":\"impact\",\"summary\":\"Draft to reject.\"}"
expect_status "create a second report" 201
REPORT2_ID=$(field data.id)

call POST "/api/v1/reports/$REPORT2_ID/submit" "$ADMIN_TOKEN"
expect_status "submit the second report" 200

call POST "/api/v1/reports/$REPORT2_ID/review" "$REVIEWER_TOKEN" '{"decision":"rejected"}'
expect_status "rejection requires notes" 422

call POST "/api/v1/reports/$REPORT2_ID/review" "$REVIEWER_TOKEN" '{"decision":"rejected","notes":"Missing supporting documents."}'
expect_status "reviewer rejects with notes" 200
check "report is rejected" 'd["data"]["status"] == "rejected"'

call PATCH "/api/v1/reports/$REPORT2_ID" "$ADMIN_TOKEN" '{"summary":"Updated after rejection."}'
expect_status "author edits a rejected report" 200

call DELETE "/api/v1/reports/$REPORT2_ID" "$ADMIN_TOKEN"
expect_status "author deletes a rejected report" 204

say "Account lifecycle and cleanup"
call DELETE "/api/v1/users/$REVIEWER_ID" "$ADMIN_TOKEN"
expect_status "superadmin deletes the reviewer" 204

call POST /api/v1/auth/refresh "" "{\"refresh_token\":\"$REVIEWER_REFRESH\"}"
expect_status "deleted user sessions are revoked" 401

call DELETE "/api/v1/roles/$SMOKE_ROLE_ID" "$ADMIN_TOKEN"
expect_status "delete the now-unassigned role" 204

call DELETE "/api/v1/events/$EVENT_ID" "$ADMIN_TOKEN"
expect_status "delete the event" 204

say "CORS and token rotation"
PREFLIGHT=$(curl -sS -o /dev/null -w '%{http_code}' -X OPTIONS "$BASE_URL/api/v1/auth/login" \
  -H "Origin: $CORS_ORIGIN" \
  -H 'Access-Control-Request-Method: POST' \
  -H 'Access-Control-Request-Headers: content-type' || true)
if [[ "$PREFLIGHT" == "204" ]]; then ok "CORS preflight from $CORS_ORIGIN is allowed (HTTP 204)"; else no "CORS preflight: expected 204, got $PREFLIGHT"; fi

call POST /api/v1/auth/refresh "" "{\"refresh_token\":\"$ADMIN_REFRESH\"}"
expect_status "rotate the admin refresh token" 200
ROTATED_ONE=$(field refresh_token)

call POST /api/v1/auth/refresh "" "{\"refresh_token\":\"$ADMIN_REFRESH\"}"
expect_status "reusing a rotated refresh token is rejected" 401

call POST /api/v1/auth/refresh "" "{\"refresh_token\":\"$ROTATED_ONE\"}"
expect_status "reuse detection revokes the whole token family" 401

call POST /api/v1/auth/login "" "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}"
expect_status "login again for logout test" 200
LOGOUT_REFRESH=$(field refresh_token)
LOGOUT_TOKEN=$(field access_token)

call POST /api/v1/auth/logout "$LOGOUT_TOKEN" "{\"refresh_token\":\"$LOGOUT_REFRESH\"}"
expect_status "logout" 204

call POST /api/v1/auth/refresh "" "{\"refresh_token\":\"$LOGOUT_REFRESH\"}"
expect_status "logged-out session cannot refresh" 401

say "Result"
printf '  %d passed, %d failed\n' "$PASS" "$FAIL"
if [[ "$FAIL" -gt 0 ]]; then exit 1; fi
