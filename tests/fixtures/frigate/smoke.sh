#!/bin/sh
# Start the fixture, wait for health, log in on loopback 8971, and fetch an authenticated snapshot.
# This script leaves the container running so the firewall probe can run afterwards.
set -eu
BASE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
COMPOSE_FILE=${FRIGATE_COMPOSE_FILE:-compose.qemu.yaml}
BASE_URL=${FRIGATE_BASE_URL:-http://127.0.0.1:8971}
CAMERA=${FRIGATE_CAMERA:-cam0}
TMP=$(mktemp -d "${TMPDIR:-/tmp}/bittern-frigate-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

fail() {
    printf 'smoke: error: %s\n' "$*" >&2
    exit 1
}

if ! command -v docker >/dev/null 2>&1; then
    fail "docker not found; this smoke test requires a Docker host"
fi
if docker compose version >/dev/null 2>&1; then
    compose() { docker compose "$@"; }
elif command -v docker-compose >/dev/null 2>&1; then
    compose() { docker-compose "$@"; }
else
    fail "docker compose is not available"
fi

cd "$BASE"
mkdir -p state/media

./check-config.sh

compose -f "$COMPOSE_FILE" up -d
CID=$(compose -f "$COMPOSE_FILE" ps -q frigate)
[ -n "$CID" ] || fail "compose did not create a frigate container"

deadline=$(( $(date +%s) + 300 ))
status=starting
while [ "$(date +%s)" -lt "$deadline" ]; do
    status=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$CID" 2>/dev/null || echo missing)
    case "$status" in
        healthy) break ;;
        unhealthy) fail "Frigate reported unhealthy; inspect docker logs $CID" ;;
    esac
    sleep 3
done
[ "$status" = healthy ] || fail "Frigate did not become healthy within 300 seconds (last: $status)"

# The image health check goes green before Frigate has created the default admin
# user, so poll for the password instead of reading the logs once. The pattern
# is a raw string: 'Password:' plus whitespace plus the hex password.
PASSWORD=""
deadline=$(( $(date +%s) + 60 ))
while [ "$(date +%s)" -lt "$deadline" ]; do
    PASSWORD=$(docker logs "$CID" 2>&1 | python3 -c '
import re
import sys
passwords = re.findall(r"Password:\s*([0-9a-fA-F]{12,})", sys.stdin.read())
print(passwords[-1] if passwords else "")
')
    [ -n "$PASSWORD" ] && break
    sleep 2
done
[ -n "$PASSWORD" ] || fail "no generated admin password found in the container logs within 60 seconds"

login_code=$(curl -sS -o "$TMP/login.json" -w '%{http_code}' \
    -c "$TMP/cookies" \
    -H 'Content-Type: application/json' \
    -d "{\"user\":\"admin\",\"password\":\"$PASSWORD\"}" \
    "$BASE_URL/api/login") || fail "login request failed"
[ "$login_code" = 200 ] || fail "login returned HTTP $login_code"

snapshot_code=000
snapshot_type=""
snapshot_size=0
deadline=$(( $(date +%s) + 60 ))
while [ "$(date +%s)" -lt "$deadline" ]; do
    read -r snapshot_code snapshot_type snapshot_size <<EOF
$(curl -sS -o "$TMP/latest.jpg" -w '%{http_code} %{content_type} %{size_download}' \
    -b "$TMP/cookies" "$BASE_URL/api/$CAMERA/latest.jpg" || true)
EOF
    if [ "$snapshot_code" = 200 ] && [ "$snapshot_size" -gt 1024 ] 2>/dev/null; then
        break
    fi
    sleep 2
done
[ "$snapshot_code" = 200 ] || fail "authenticated snapshot returned HTTP $snapshot_code"
[ "$snapshot_size" -gt 1024 ] 2>/dev/null || fail "authenticated snapshot was too small: $snapshot_size bytes"
case "$snapshot_type" in
    image/jpeg*) ;;
    *) fail "authenticated snapshot has unexpected content type: $snapshot_type" ;;
esac

unauthenticated=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/api/$CAMERA/latest.jpg") || true
[ "$unauthenticated" != 200 ] || fail "8971 served a snapshot without authentication"

printf 'smoke: ok: healthy, authenticated snapshot HTTP %s, %s bytes, %s\n' \
    "$snapshot_code" "$snapshot_size" "$snapshot_type"
printf 'smoke: container left running as %s; run the firewall probe next\n' "$CID"
