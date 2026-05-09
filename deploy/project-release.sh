#!/usr/bin/env bash
set -euo pipefail

# 1. 只负责把当前 release 目录里的编排资产和镜像包应用到运行目录。
# 2. 不负责生成镜像，也不负责计算影响范围；这些工作由构建机和 impact-rules.sh 提前完成。
# 3. 如果 release-plan.env 缺失或格式错误，直接失败，避免在生产机上猜测要重启哪些服务。

runtime_dir="${SMARTFLOW_RUNTIME_DIR:-/root/smartflow}"
release_dir="${SMARTFLOW_RELEASE_DIR:?SMARTFLOW_RELEASE_DIR is required}"
release_id="${SMARTFLOW_RELEASE_ID:?SMARTFLOW_RELEASE_ID is required}"
plan_file="${release_dir}/deploy/release-plan.env"
runtime_env="${runtime_dir}/.env"

source "${release_dir}/deploy/service-catalog.sh"

if [[ ! -f "${plan_file}" ]]; then
  echo "release plan not found: ${plan_file}" >&2
  exit 66
fi

if [[ ! -f "${runtime_env}" ]]; then
  echo "runtime env not found: ${runtime_env}" >&2
  exit 67
fi

source "${plan_file}"

if [[ "${SMARTFLOW_NOOP:-0}" == "1" ]]; then
  echo "release ${release_id} resolved to no-op"
  exit 0
fi

set_env_var() {
  local key="$1"
  local value="$2"
  local file="$3"
  local tmp

  tmp="$(mktemp)"
  awk -v key="${key}" -v value="${value}" '
    BEGIN { updated = 0 }
    $0 ~ ("^" key "=") {
      print key "=" value
      updated = 1
      next
    }
    { print }
    END {
      if (updated == 0) {
        print key "=" value
      }
    }
  ' "${file}" > "${tmp}"
  mv "${tmp}" "${file}"
}

mkdir -p "${runtime_dir}/deploy/nginx"

install -m 0644 "${release_dir}/docker-compose.full.yml" "${runtime_dir}/docker-compose.full.yml"
install -m 0644 "${release_dir}/deploy/nginx/default.conf" "${runtime_dir}/deploy/nginx/default.conf"

if compgen -G "${release_dir}/.docker-bundles/*.tar" >/dev/null 2>&1; then
  bash "${release_dir}/deploy/docker-load.sh" "${release_dir}/.docker-bundles"
fi

services=()
IFS=',' read -r -a raw_services <<< "${SMARTFLOW_RESTART_SERVICES:-}"
for service in "${raw_services[@]}"; do
  if [[ -n "${service}" ]]; then
    services+=("${service}")
  fi
done

if [[ "${#services[@]}" -eq 0 ]]; then
  echo "release ${release_id} has no target services"
  exit 0
fi

# 1. release-plan.env 是构建机生成的单一事实源，部署机只按服务名读取对应镜像变量。
# 2. 某个服务缺少镜像变量时直接失败，避免 compose 沿用旧镜像造成“发布成功但代码未更新”。
# 3. .env 更新发生在 docker load 之后；如果镜像包无法加载，不会提前切换运行时镜像引用。
for service in "${services[@]}"; do
  image_env="$(smartflow_image_env_for_service "${service}")"
  local_image_ref="${!image_env:-}"
  if [[ -z "${local_image_ref}" ]]; then
    echo "image ref not found in release plan: ${image_env}" >&2
    exit 68
  fi
  set_env_var "${image_env}" "${local_image_ref}" "${runtime_env}"
done

# 1. 使用 --no-deps 只重建命中的服务，避免后端小改动把整套依赖链一起拉起来。
# 2. 如果后续服务新增或删减，只要 release-plan.env 给出的服务名同步更新，这里无需改脚本。
# 3. 失败时直接退出，由上层薄封装决定是否切回旧 release。
(
  cd "${runtime_dir}"
  docker compose -f docker-compose.full.yml up -d --no-deps "${services[@]}"
)

if [[ "${SMARTFLOW_BUILD_BACKEND:-0}" == "1" || " ${services[*]} " == *" api "* ]]; then
  curl -fsS http://127.0.0.1:8080/api/v1/health >/dev/null
fi

if [[ " ${services[*]} " == *" frontend "* ]]; then
  curl -k -fsS https://smartmate.lecspace.com/ >/dev/null
  curl -k -fsS https://git.lecspace.com/ >/dev/null
fi

printf '%s\n' "${release_id}" > "${SMARTFLOW_RELEASE_ROOT}/last_successful_release"
ln -sfn "${release_dir}" "${SMARTFLOW_CURRENT_LINK}"
echo "release ${release_id} deployed successfully"
