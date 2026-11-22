#!/bin/sh
# Copy repository-owned templates, defaults, the JSON Schema, and the UI assets
# into the Go module.
#
# go:embed cannot reach outside agent/, so the Go build needs a local copy.
# This script is idempotent and rewrites the generated tree on every run. The
# generated tree is gitignored and must never be edited by hand.
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
dest="$repo_root/agent/internal/assets"

rm -rf "$dest/templates" "$dest/defaults" "$dest/schema" "$dest/ui"
mkdir -p "$dest/templates" "$dest/defaults" "$dest/schema" "$dest/ui"

# Copy templates only. templates/README.md documents the directory rather than
# feeding the renderer, so it stays out of the embedded tree.
find "$repo_root/templates" -type f ! -name 'README.md' -print | while IFS= read -r source; do
    relative=${source#"$repo_root/templates/"}
    mkdir -p "$dest/templates/$(dirname -- "$relative")"
    cp "$source" "$dest/templates/$relative"
done

# Copy product defaults only. defaults/README.md documents the directory and
# is not a runtime asset.
find "$repo_root/defaults" -type f ! -name 'README.md' -print | while IFS= read -r source; do
    relative=${source#"$repo_root/defaults/"}
    mkdir -p "$dest/defaults/$(dirname -- "$relative")"
    cp "$source" "$dest/defaults/$relative"
done

# Copy the UI templates and static files. The UI README documents the
# directory and is not a runtime asset.
find "$repo_root/ui/templates" "$repo_root/ui/static" -type f ! -name 'README.md' -print | while IFS= read -r source; do
    relative=${source#"$repo_root/ui/"}
    mkdir -p "$dest/ui/$(dirname -- "$relative")"
    cp "$source" "$dest/ui/$relative"
done

cp "$repo_root/schema/appliance.schema.json" "$dest/schema/appliance.schema.json"
