#!/usr/bin/env bash
set -euo pipefail

APP_TAG="latest"
OUTPUT_DIR=".docker-bundles"
INCLUDE_INFRA=0
PLAN_FILE=""
SERVICES_CSV=""
SKIP_BACKEND=0
SKIP_FRONTEND=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --app-tag)
      APP_TAG="$2"
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
    --plan-file)
      PLAN_FILE="$2"
      shift 2
      ;;
    --services)
      SERVICES_CSV="$2"
      shift 2
      ;;
    --skip-backend)
      SKIP_BACKEND=1
      shift
      ;;
    --skip-frontend)
      SKIP_FRONTEND=1
      shift
      ;;
    --backend-image|--frontend-image)
      # 兼容旧调用参数。服务级发布后镜像引用来自 release-plan.env，不再由统一 backend/frontend 参数决定。
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 64
      ;;
  esac
done

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${repo_root}/deploy/service-catalog.sh"

bundle_dir="${repo_root}/${OUTPUT_DIR}"
app_bundle_path="${bundle_dir}/smartflow-app-${APP_TAG}.tar"
infra_bundle_path="${bundle_dir}/smartflow-infra-${APP_TAG}.tar"

mkdir -p "${bundle_dir}"
rm -f "${app_bundle_path}"

if [[ -n "${PLAN_FILE}" ]]; then
  # 1. 构建机只信任影响分析生成的计划文件，避免 workflow 和脚本重复计算影响范围。
  # 2. plan 文件缺失时直接失败，防止误把默认全量构建当成精准发布。
  # 3. SMARTFLOW_APP_TAG 优先级高于命令行参数，保证镜像 tag 与 release id 一致。
  source "${PLAN_FILE}"
  APP_TAG="${SMARTFLOW_APP_TAG:-${APP_TAG}}"
  SERVICES_CSV="${SMARTFLOW_RESTART_SERVICES:-${SERVICES_CSV}}"
  app_bundle_path="${bundle_dir}/smartflow-app-${APP_TAG}.tar"
  infra_bundle_path="${bundle_dir}/smartflow-infra-${APP_TAG}.tar"
  rm -f "${app_bundle_path}"
fi

declare -a services=()
if [[ -n "${SERVICES_CSV}" ]]; then
  IFS=',' read -r -a services <<< "${SERVICES_CSV}"
elif [[ -z "${PLAN_FILE}" ]]; then
  # 1. 手工执行且没有传 plan 时，默认构建所有应用服务，保持旧脚本“一键全量打包”的可用性。
  # 2. 这里改为服务级镜像全量，而不是 backend-suite，便于后续逐步淘汰单体后端镜像。
  if [[ "${SKIP_BACKEND}" -eq 0 ]]; then
    services+=("${SMARTFLOW_BACKEND_SERVICES[@]}")
  fi
  if [[ "${SKIP_FRONTEND}" -eq 0 ]]; then
    services+=("frontend")
  fi
fi

image_ref_for_service() {
  local service="$1"
  local image_env
  local image_ref

  image_env="$(smartflow_image_env_for_service "${service}")"
  local -n image_value="${image_env}"
  image_ref="${image_value:-}"
  if [[ -z "${image_ref}" ]]; then
    image_ref="$(smartflow_default_image_for_service "${service}" "${APP_TAG}")"
  fi

  printf '%s\n' "${image_ref}"
}

declare -a app_images=()
for service in "${services[@]}"; do
  if [[ -z "${service}" ]]; then
    continue
  fi

  if [[ "${service}" == "frontend" ]]; then
    if [[ "${SKIP_FRONTEND}" -eq 1 ]]; then
      continue
    fi

    image_ref="$(image_ref_for_service "${service}")"
    echo "==> Build frontend image ${image_ref}"
    docker build --platform linux/amd64 -f "${repo_root}/frontend/Dockerfile" -t "${image_ref}" "${repo_root}/frontend"
    app_images+=("${image_ref}")
    continue
  fi

  if [[ "${SKIP_BACKEND}" -eq 1 ]]; then
    continue
  fi

  if ! smartflow_is_backend_service "${service}"; then
    echo "unknown release service: ${service}" >&2
    exit 65
  fi

  image_ref="$(image_ref_for_service "${service}")"
  echo "==> Build backend service image ${image_ref}"
  docker build \
    --platform linux/amd64 \
    --target runtime-service \
    --build-arg "SERVICE=${service}" \
    -f "${repo_root}/backend/Dockerfile" \
    -t "${image_ref}" \
    "${repo_root}/backend"
  app_images+=("${image_ref}")
done

if [[ "${#app_images[@]}" -gt 0 ]]; then
  echo "==> Export app bundle to ${app_bundle_path}"
  docker save -o "${app_bundle_path}" "${app_images[@]}"
else
  echo "==> Skip app bundle export because no application image is selected"
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
