#!/bin/sh
# Run the upstream Frigate config check before starting the fixture.
set -eu
BASE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
IMAGE=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["image"]["reference"])' \
    "$BASE/manifest.json")

if ! command -v docker >/dev/null 2>&1; then
    echo "check-config: docker not found; this check requires a Docker host" >&2
    exit 2
fi

exec docker run --rm --network none --platform linux/amd64 \
    -v "$BASE/config:/config:ro" \
    --entrypoint python3 \
    "$IMAGE" \
    -u -m frigate --validate-config
