#!/usr/bin/env sh

set -eu

BUNDLE_DIR="${1:-$(cd "$(dirname "$0")/.." && pwd)/.docker-bundles}"

if [ ! -d "$BUNDLE_DIR" ]; then
  echo "Bundle directory not found: $BUNDLE_DIR" >&2
  exit 1
fi

found_bundle=0

for bundle in "$BUNDLE_DIR"/*.tar; do
  if [ ! -e "$bundle" ]; then
    continue
  fi

  found_bundle=1
  echo "==> Load bundle $bundle"
  docker load -i "$bundle"
done

if [ "$found_bundle" -eq 0 ]; then
  echo "No tar bundles found in: $BUNDLE_DIR" >&2
  exit 1
fi

echo "==> Done"
