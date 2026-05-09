#!/usr/bin/env bash
set -euo pipefail

APP_TAG="latest"
BACKEND_IMAGE="smartflow/backend-suite"
FRONTEND_IMAGE="smartflow/frontend"
OUTPUT_DIR=".docker-bundles"
INCLUDE_INFRA=0
SKIP_BACKEND=0
SKIP_FRONTEND=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --app-tag)
      APP_TAG="$2"
      shift 2
      ;;
    --backend-image)
      BACKEND_IMAGE="$2"
      shift 2
      ;;
    --frontend-image)
      FRONTEND_IMAGE="$2"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="$2"
      shift 2
      ;;
    --include-infra)
      INCLUDE_INFRA=1
      shift
      ;;
    --skip-backend)
      SKIP_BACKEND=1
      shift
      ;;
    --skip-frontend)
      SKIP_FRONTEND=1
      shift
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 64
      ;;
  esac
done

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bundle_dir="${repo_root}/${OUTPUT_DIR}"
backend_ref="${BACKEND_IMAGE}:${APP_TAG}"
frontend_ref="${FRONTEND_IMAGE}:${APP_TAG}"
app_bundle_path="${bundle_dir}/smartflow-app-${APP_TAG}.tar"
infra_bundle_path="${bundle_dir}/smartflow-infra-${APP_TAG}.tar"

mkdir -p "${bundle_dir}"

if [[ "${SKIP_BACKEND}" -eq 0 ]]; then
  echo "==> Build backend image ${backend_ref}"
  docker build --platform linux/amd64 -f "${repo_root}/backend/Dockerfile" -t "${backend_ref}" "${repo_root}/backend"
fi

if [[ "${SKIP_FRONTEND}" -eq 0 ]]; then
  echo "==> Build frontend image ${frontend_ref}"
  docker build --platform linux/amd64 -f "${repo_root}/frontend/Dockerfile" -t "${frontend_ref}" "${repo_root}/frontend"
fi

declare -a app_images=()
if [[ "${SKIP_BACKEND}" -eq 0 ]]; then
  app_images+=("${backend_ref}")
fi
if [[ "${SKIP_FRONTEND}" -eq 0 ]]; then
  app_images+=("${frontend_ref}")
fi

if [[ "${#app_images[@]}" -gt 0 ]]; then
  rm -f "${app_bundle_path}"
  echo "==> Export app bundle to ${app_bundle_path}"
  docker save -o "${app_bundle_path}" "${app_images[@]}"
else
  echo "==> Skip app bundle export because backend/frontend are both skipped"
fi

if [[ "${INCLUDE_INFRA}" -eq 0 ]]; then
  echo "==> Done."
  exit 0
fi

infra_images=(
  "${SMARTFLOW_MYSQL_IMAGE:-mysql:8.0}"
  "${SMARTFLOW_REDIS_IMAGE:-redis:7}"
  "${SMARTFLOW_KAFKA_IMAGE:-apache/kafka:3.7.2}"
  "${SMARTFLOW_ETCD_IMAGE:-quay.io/coreos/etcd:v3.5.5}"
  "${SMARTFLOW_MINIO_IMAGE:-minio/minio:RELEASE.2023-03-20T20-16-18Z}"
  "${SMARTFLOW_MILVUS_IMAGE:-milvusdb/milvus:v2.4.4}"
  "${SMARTFLOW_ATTU_IMAGE:-zilliz/attu:v2.4.3}"
)

for image_ref in "${infra_images[@]}"; do
  if docker image inspect "${image_ref}" >/dev/null 2>&1; then
    echo "==> Reuse local infra image ${image_ref}"
    continue
  fi

  echo "==> Pull infra image ${image_ref}"
  docker pull "${image_ref}"
done

rm -f "${infra_bundle_path}"
echo "==> Export infra bundle to ${infra_bundle_path}"
docker save -o "${infra_bundle_path}" "${infra_images[@]}"

echo "==> Done."
