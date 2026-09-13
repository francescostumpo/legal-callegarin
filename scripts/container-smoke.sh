#!/bin/sh
set -eu

SMOKE_IMAGE=${1:-legal-callegarin:local}
SMOKE_CONTAINER=legal-callegarin-smoke-$$
SMOKE_CONTAINER_ID=
SMOKE_ORIGIN=http://127.0.0.1:18080
SMOKE_PASSWORD=smoke-password-non-production
SMOKE_PASSWORD_HASH='$argon2id$v=19$m=19456,t=2,p=1$owm1iA2MDURV4+KmH+qBvw$ynsiPTPiLIlPJLtgx8Xfx2/GdHCZwRZxDzjR+nlLlmE'
SMOKE_SESSION_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
SMOKE_COMMIT=$(git rev-parse HEAD)

if [ "${#SMOKE_COMMIT}" -ne 40 ]; then
  echo "unable to resolve an exact Git commit" >&2
  exit 1
fi
case "$SMOKE_COMMIT" in
  *[!0-9a-f]*)
    echo "resolved Git commit is not canonical" >&2
    exit 1
    ;;
esac
SMOKE_STATUS=$(git status --porcelain --untracked-files=all)
if [ -n "$SMOKE_STATUS" ]; then
  echo "container smoke requires a clean Git worktree" >&2
  exit 1
fi

SMOKE_DIR=$(mktemp -d)
cleanup() {
  if [ -n "$SMOKE_CONTAINER_ID" ]; then
    docker rm -f "$SMOKE_CONTAINER_ID" >/dev/null 2>&1 || true
  fi
  rm -rf "$SMOKE_DIR"
}
trap cleanup EXIT INT TERM

http() {
  curl --connect-timeout 2 --max-time 5 "$@"
}

docker build --build-arg VERSION=local-test --build-arg COMMIT="$SMOKE_COMMIT" -t "$SMOKE_IMAGE" .

started_at=$(date +%s%3N)
SMOKE_CONTAINER_ID=$(docker create \
  --name "$SMOKE_CONTAINER" \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --stop-timeout 25 \
  -p 127.0.0.1:18080:8080 \
  --env APP_ENV=development \
  --env STORAGE_MODE=memory \
  --env "PUBLIC_BASE_URL=$SMOKE_ORIGIN" \
  --env ADMIN_USERNAME=smoke-admin \
  --env "ADMIN_PASSWORD_HASH=$SMOKE_PASSWORD_HASH" \
  --env "SESSION_KEY_BASE64=$SMOKE_SESSION_KEY" \
  "$SMOKE_IMAGE")
test -n "$SMOKE_CONTAINER_ID"
docker start "$SMOKE_CONTAINER_ID" >/dev/null

ready=false
for attempt in $(seq 1 60); do
  if http -fsS "$SMOKE_ORIGIN/health/live" >/dev/null; then
    ready=true
    break
  fi
  sleep 0.25
done
if [ "$ready" != true ]; then
  docker logs "$SMOKE_CONTAINER_ID" >&2 || true
  echo "container did not become live within the bounded retry window" >&2
  exit 1
fi
ready_at=$(date +%s%3N)
echo "cold_start_readiness_ms=$((ready_at - started_at))"

http -fsS -o "$SMOKE_DIR/readiness" "$SMOKE_ORIGIN/health/ready"
grep -qx 'ok' "$SMOKE_DIR/readiness"
http -fsS -D "$SMOKE_DIR/public.headers" -o "$SMOKE_DIR/public.html" "$SMOKE_ORIGIN/"
grep -qi '^Content-Security-Policy:' "$SMOKE_DIR/public.headers"
grep -qi '^X-Content-Type-Options: nosniff' "$SMOKE_DIR/public.headers"
if ! grep -Eqi '^Referrer-Policy:[[:space:]]*same-origin[[:space:]]*$' "$SMOKE_DIR/public.headers"; then
  echo "public response Referrer-Policy is not exactly same-origin" >&2
  exit 1
fi
grep -qi '^Permissions-Policy: camera=(), geolocation=(), microphone=()' "$SMOKE_DIR/public.headers"
grep -qi '^X-Frame-Options: DENY' "$SMOKE_DIR/public.headers"
if grep -qi '^Strict-Transport-Security:' "$SMOKE_DIR/public.headers"; then
  echo "development response unexpectedly enabled HSTS" >&2
  exit 1
fi
if grep -qi '^Set-Cookie:' "$SMOKE_DIR/public.headers"; then
  echo "public response unexpectedly set a cookie" >&2
  exit 1
fi
grep -q 'Studio Legale Alessandro Callegarin' "$SMOKE_DIR/public.html"
public_asset=$(grep -o '/assets/site-[0-9a-f]\{12\}\.css' "$SMOKE_DIR/public.html" | sed -n '1p')
test -n "$public_asset"
http -fsS "$SMOKE_ORIGIN$public_asset" >/dev/null

http -fsS -o "$SMOKE_DIR/login.html" "$SMOKE_ORIGIN/admin/login"
grep -q '<form method="post" action="/admin/login">' "$SMOKE_DIR/login.html"
started=$(sed -n 's/.*name="started" value="\([^"]*\)".*/\1/p' "$SMOKE_DIR/login.html" | sed -n '1p')
test -n "$started"
login_status=$(http -sS \
  -D "$SMOKE_DIR/login.headers" \
  -o "$SMOKE_DIR/login.response" \
  -w '%{http_code}' \
  -H "Origin: $SMOKE_ORIGIN" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode 'username=smoke-admin' \
  --data-urlencode "password=$SMOKE_PASSWORD" \
  --data-urlencode "started=$started" \
  "$SMOKE_ORIGIN/admin/login")
test "$login_status" = 303
session_cookie=$(sed -n 's/^Set-Cookie: \([^;]*\).*/\1/p' "$SMOKE_DIR/login.headers" | tr -d '\r' | sed -n '1p')
test -n "$session_cookie"
http -fsS -H "Cookie: $session_cookie" -o "$SMOKE_DIR/admin.html" "$SMOKE_ORIGIN/admin"
admin_asset=$(grep -o '/admin/assets/index-[A-Za-z0-9_-]*\.js' "$SMOKE_DIR/admin.html" | sed -n '1p')
test -n "$admin_asset"
http -fsS -H "Cookie: $session_cookie" "$SMOKE_ORIGIN$admin_asset" >/dev/null

api_status=$(http -sS -o "$SMOKE_DIR/api.json" -w '%{http_code}' "$SMOKE_ORIGIN/api/admin/session")
test "$api_status" = 401
grep -q '"code":"authentication_required"' "$SMOKE_DIR/api.json"
grep -Eq '"requestId":"[^"]+"' "$SMOKE_DIR/api.json"
grep -q '"fields":{}' "$SMOKE_DIR/api.json"

startup_log=$(docker logs "$SMOKE_CONTAINER_ID" 2>&1)
printf '%s\n' "$startup_log" | grep -q '"event":"server_starting"'
printf '%s\n' "$startup_log" | grep -q '"version":"local-test"'
printf '%s\n' "$startup_log" | grep -q "\"commit\":\"$SMOKE_COMMIT\""
if printf '%s\n' "$startup_log" | grep -Fq "$SMOKE_SESSION_KEY"; then
  echo "startup log leaked the synthetic session fixture" >&2
  exit 1
fi
if printf '%s\n' "$startup_log" | grep -Fq "$SMOKE_PASSWORD_HASH"; then
  echo "startup log leaked the synthetic password hash fixture" >&2
  exit 1
fi

stop_started_at=$(date +%s%3N)
docker stop -t 25 "$SMOKE_CONTAINER_ID" >/dev/null
stop_finished_at=$(date +%s%3N)
echo "graceful_stop_ms=$((stop_finished_at - stop_started_at))"

exit_code=$(docker inspect --format '{{.State.ExitCode}}' "$SMOKE_CONTAINER_ID")
oom_killed=$(docker inspect --format '{{.State.OOMKilled}}' "$SMOKE_CONTAINER_ID")
test "$exit_code" = 0
test "$oom_killed" = false

echo "container smoke passed (synthetic non-production credentials only)"
