#!/usr/bin/env bash
set -euo pipefail

release_dir=""
plan_file=""
bundle_dir=".docker-bundles"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --release-dir)
      release_dir="$2"
      shift 2
      ;;
    --plan-file)
      plan_file="$2"
      shift 2
      ;;
    --bundle-dir)
      bundle_dir="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 64
      ;;
  esac
done

if [[ -z "${release_dir}" || -z "${plan_file}" ]]; then
  echo "usage: stage-release.sh --release-dir <dir> --plan-file <file> [--bundle-dir <dir>]" >&2
  exit 64
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
release_abs="${repo_root}/${release_dir}"
bundle_abs="${repo_root}/${bundle_dir}"

rm -rf "${release_abs}"
mkdir -p "${release_abs}/deploy/nginx" "${release_abs}/deploy/certs" "${release_abs}/.docker-bundles"

cp "${repo_root}/docker-compose.full.yml" "${release_abs}/docker-compose.full.yml"
cp "${repo_root}/deploy/docker-load.sh" "${release_abs}/deploy/docker-load.sh"
cp "${repo_root}/deploy/project-release.sh" "${release_abs}/deploy/project-release.sh"
cp "${repo_root}/deploy/project-rollback.sh" "${release_abs}/deploy/project-rollback.sh"
cp "${repo_root}/deploy/impact-rules.sh" "${release_abs}/deploy/impact-rules.sh"
cp "${repo_root}/deploy/nginx/default.conf" "${release_abs}/deploy/nginx/default.conf"
cp "${repo_root}/deploy/certs/README.md" "${release_abs}/deploy/certs/README.md"
cp "${plan_file}" "${release_abs}/deploy/release-plan.env"

if compgen -G "${bundle_abs}/*.tar" >/dev/null 2>&1; then
  cp "${bundle_abs}"/*.tar "${release_abs}/.docker-bundles/"
fi
