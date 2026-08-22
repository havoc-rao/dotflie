#!/usr/bin/env bash
# 首次装机一键部署:构建安装 → 若无清单则生成模板,否则直接链接。
# 安装目录可用 DOTFILES_PREFIX 覆盖。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

"$ROOT/scripts/install.sh" "$@"

cd "$ROOT"
if [ ! -f .dotfiles.yaml ]; then
  "$ROOT/dist/dotf" init
  echo
  echo "==> 已生成 .dotfiles.yaml,编辑清单后运行:"
  echo "    dotf link"
else
  "$ROOT/dist/dotf" link
fi
