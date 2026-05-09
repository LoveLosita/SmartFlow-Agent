#!/usr/bin/env bash
set -euo pipefail

base_ref="${1:-}"
head_ref="${2:-HEAD}"
output_file="${3:-}"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

backend_services=(userauth notification active-scheduler schedule task task-class course memory agent taskclassforum tokenstore llm api)
selected_services=()
frontend_changed=0
full_backend=0

add_service() {
  local service="$1"
  for current in "${selected_services[@]}"; do
    if [[ "${current}" == "${service}" ]]; then
      return 0
    fi
  done
  selected_services+=("${service}")
}

have_ref() {
  local ref="$1"
  [[ -n "${ref}" ]] && git rev-parse --verify --quiet "${ref}^{commit}" >/dev/null
}

if ! have_ref "${head_ref}"; then
  echo "head ref not found: ${head_ref}" >&2
  exit 65
fi

app_tag="$(git rev-parse --short=12 "${head_ref}")"

declare -a changed_files=()
if have_ref "${base_ref}"; then
  mapfile -t changed_files < <(git diff --name-only "${base_ref}" "${head_ref}")
else
  frontend_changed=1
  full_backend=1
fi

for file in "${changed_files[@]}"; do
  case "${file}" in
    README.md|docs/*)
      ;;
    frontend/*|deploy/nginx/*)
      frontend_changed=1
      ;;
    deploy/docker-pack.*|deploy/docker-load.sh|deploy/stage-release.sh|deploy/project-release.sh|deploy/project-rollback.sh|deploy/impact-rules.sh)
      frontend_changed=1
      full_backend=1
      ;;
    docker-compose.full.yml|frontend/nginx.conf|.env.full.example)
      frontend_changed=1
      full_backend=1
      ;;
    backend/Dockerfile|backend/config.docker.yaml|backend/shared/*|backend/client/*)
      full_backend=1
      ;;
    backend/gateway/*|backend/cmd/api/*)
      add_service "api"
      ;;
    backend/cmd/userauth/*|backend/services/userauth/*)
      add_service "userauth"
      ;;
    backend/cmd/notification/*|backend/services/notification/*)
      add_service "notification"
      ;;
    backend/cmd/active-scheduler/*|backend/services/active_scheduler/*)
      add_service "active-scheduler"
      ;;
    backend/cmd/schedule/*|backend/services/schedule/*)
      add_service "schedule"
      ;;
    backend/cmd/task/*|backend/services/task/*)
      add_service "task"
      ;;
    backend/cmd/task-class/*|backend/services/task_class/*)
      add_service "task-class"
      ;;
    backend/cmd/course/*|backend/services/course/*)
      add_service "course"
      ;;
    backend/cmd/memory/*|backend/services/memory/*)
      add_service "memory"
      ;;
    backend/cmd/agent/*|backend/services/agent/*)
      add_service "agent"
      ;;
    backend/cmd/taskclassforum/*|backend/services/taskclassforum/*)
      add_service "taskclassforum"
      ;;
    backend/cmd/tokenstore/*|backend/services/tokenstore/*)
      add_service "tokenstore"
      ;;
    backend/cmd/llm/*|backend/services/llm/*)
      add_service "llm"
      ;;
    backend/*)
      full_backend=1
      ;;
  esac
done

if [[ "${full_backend}" -eq 1 ]]; then
  selected_services=("${backend_services[@]}")
fi

if [[ "${frontend_changed}" -eq 1 ]]; then
  add_service "frontend"
fi

build_backend=0
build_frontend=0
for service in "${selected_services[@]}"; do
  if [[ "${service}" == "frontend" ]]; then
    build_frontend=1
  else
    build_backend=1
  fi
done

noop=0
if [[ "${#selected_services[@]}" -eq 0 ]]; then
  noop=1
fi

restart_csv=""
if [[ "${#selected_services[@]}" -gt 0 ]]; then
  restart_csv="$(IFS=,; echo "${selected_services[*]}")"
fi

content=$(
  cat <<EOF
SMARTFLOW_APP_TAG=${app_tag}
SMARTFLOW_NOOP=${noop}
SMARTFLOW_BUILD_BACKEND=${build_backend}
SMARTFLOW_BUILD_FRONTEND=${build_frontend}
SMARTFLOW_BACKEND_IMAGE=smartflow/backend-suite:${app_tag}
SMARTFLOW_FRONTEND_IMAGE=smartflow/frontend:${app_tag}
SMARTFLOW_RESTART_SERVICES=${restart_csv}
EOF
)

if [[ -n "${output_file}" ]]; then
  printf '%s\n' "${content}" > "${output_file}"
else
  printf '%s\n' "${content}"
fi
