#!/usr/bin/env bash
set -euo pipefail

# 1. 发布脚本共享同一份服务清单，避免影响计算、镜像构建、部署更新三处各自维护服务名。
# 2. 这里只把文本清单加载成 Bash 可用的数据结构；不负责判断某次发布要构建哪些服务。
# 3. 新增或删除后端服务时，优先改 service-catalog.txt，再同步 compose 服务定义，避免脚本之间出现漂移。
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
catalog_file="${SMARTFLOW_SERVICE_CATALOG_FILE:-${script_dir}/service-catalog.txt}"

if [[ ! -f "${catalog_file}" ]]; then
  echo "service catalog not found: ${catalog_file}" >&2
  exit 66
fi

SMARTFLOW_BACKEND_SERVICES=()
declare -Ag SMARTFLOW_SERVICE_IMAGE_ENVS=()
declare -Ag SMARTFLOW_SERVICE_IMAGE_REPOS=()
declare -Ag SMARTFLOW_SERVICE_KINDS=()

while IFS='|' read -r service image_env image_repo kind; do
  if [[ -z "${service}" || "${service}" == \#* ]]; then
    continue
  fi

  SMARTFLOW_SERVICE_IMAGE_ENVS["${service}"]="${image_env}"
  SMARTFLOW_SERVICE_IMAGE_REPOS["${service}"]="${image_repo}"
  SMARTFLOW_SERVICE_KINDS["${service}"]="${kind}"

  if [[ "${kind}" == "backend" ]]; then
    SMARTFLOW_BACKEND_SERVICES+=("${service}")
  fi
done < "${catalog_file}"

# smartflow_is_backend_service 负责判断服务是否属于后端发布粒度。
# 输入：compose 服务名。
# 输出：命中返回 0，未命中返回 1；不负责校验 frontend 这类非后端服务。
smartflow_is_backend_service() {
  local service="$1"
  [[ "${SMARTFLOW_SERVICE_KINDS[${service}]-}" == "backend" ]]
}

# smartflow_image_env_for_service 负责把 compose 服务名映射成运行时 .env 里的镜像变量。
# 输入：compose 服务名，例如 task-class。
# 输出：变量名，例如 SMARTFLOW_IMAGE_TASK_CLASS；未知服务直接失败，避免发布脚本静默写错键。
smartflow_image_env_for_service() {
  local service="$1"
  local image_env="${SMARTFLOW_SERVICE_IMAGE_ENVS[${service}]-}"

  if [[ -z "${image_env}" ]]; then
    echo "unknown service: ${service}" >&2
    return 65
  fi

  echo "${image_env}"
}

# smartflow_default_image_for_service 负责生成服务级镜像的默认引用。
# 输入：compose 服务名、应用 tag。
# 输出：smartflow/<service>:<tag>，frontend 保持 smartflow/frontend:<tag>。
smartflow_default_image_for_service() {
  local service="$1"
  local app_tag="$2"
  local image_repo="${SMARTFLOW_SERVICE_IMAGE_REPOS[${service}]-}"

  if [[ -z "${image_repo}" ]]; then
    echo "unknown service: ${service}" >&2
    return 65
  fi

  echo "${image_repo}:${app_tag}"
}
