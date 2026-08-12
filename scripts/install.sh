#!/usr/bin/env bash
# 构建并安装 dotf 到本机。
# 安装目录默认 ~/.local/bin,可用 DOTFILES_PREFIX 覆盖。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PREFIX="${DOTFILES_PREFIX:-$HOME/.local/bin}"

"$ROOT/scripts/build.sh"

install -d "$PREFIX"
install -m 0755 "$ROOT/bin/dotf" "$PREFIX/dotf"
echo "==> installed: $PREFIX/dotf"
"$PREFIX/dotf" --version

if ! case ":$PATH:" in *":$PREFIX:"*) ;; *) false ;; esac; then
  echo
  echo "!! $PREFIX 不在 PATH 中,请在 ~/.zshrc 添加:"
  echo "   export PATH=\"$PREFIX:\$PATH\""
fi
