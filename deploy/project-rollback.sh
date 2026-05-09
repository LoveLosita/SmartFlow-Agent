#!/usr/bin/env bash
set -euo pipefail

# 1. 回滚本质上就是把目标旧 release 再部署一遍。
# 2. 这里不单独复制逻辑，避免发布和回滚两套脚本漂移。
# 3. 薄封装脚本传入哪个 release_id，这里就把哪个 release 当成目标版本重放。

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "${script_dir}/project-release.sh" "$@"
