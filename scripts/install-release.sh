#!/usr/bin/env bash
# 从 GitHub Releases 下载预编译二进制并安装 dotf（无需 Go 工具链）。
# 安装目录默认 ~/.local/bin，可用 DOTFILES_PREFIX 覆盖（与 scripts/install.sh 一致）。
# 用法:
#   curl -fsSL https://raw.githubusercontent.com/havoc-rao/dotflie/main/scripts/install-release.sh | bash
#   DOTFILES_PREFIX=~/bin curl -fsSL https://raw.githubusercontent.com/havoc-rao/dotflie/main/scripts/install-release.sh | bash
set -euo pipefail

REPO="havoc-rao/dotflie"   # GitHub owner/repo（dotflie 为发布仓库名）
PROJECT="dotflie"          # goreleaser project_name，决定产物文件名
BINARY="dotf"
PREFIX="${DOTFILES_PREFIX:-$HOME/.local/bin}"

# ---- 1. 探测平台（goreleaser 交叉编译矩阵: linux/darwin/windows × amd64/arm64）----
case "$(uname -s)" in
  Linux*)            OS="linux" ;;
  Darwin*)           OS="darwin" ;;
  MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
  *) echo "!! 不支持的 OS: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64)      ARCH="amd64" ;;
  aarch64|arm64)     ARCH="arm64" ;;
  *) echo "!! 不支持的架构: $(uname -m)" >&2; exit 1 ;;
esac
if [ "$OS" = windows ] && [ "$ARCH" = arm64 ]; then
  echo "!! windows/arm64 无发布产物" >&2; exit 1
fi
[ "$OS" = windows ] && BINARY="${BINARY}.exe"

# ---- 2. 解析最新版本（latest 页重定向到 vX.Y.Z tag；可用 DOTF_VERSION 固定版本）----
VERSION="${DOTF_VERSION:-}"
if [ -z "$VERSION" ]; then
  VERSION="$(curl -fsSL -o /dev/null -w '%{url_effective}' \
    "https://github.com/$REPO/releases/latest" | sed -n 's#.*/tag/v##p')"
fi
[ -n "$VERSION" ] || { echo "!! 无法解析最新版本号" >&2; exit 1; }
echo "==> 下载 dotf v$VERSION ($OS/$ARCH)"

# ---- 3. 下载产物与校验和，校验后解压 ----
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

BASE_URL="https://github.com/$REPO/releases/download/v$VERSION"
ASSET="${PROJECT}_${VERSION}_${OS}_${ARCH}.tar.gz"
[ "$OS" = windows ] && ASSET="${PROJECT}_${VERSION}_${OS}_${ARCH}.zip"

curl -fsSL -o "$TMP/$ASSET"   "$BASE_URL/$ASSET"
curl -fsSL -o "$TMP/checksums.txt" "$BASE_URL/checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  ( cd "$TMP" && grep " $ASSET$" checksums.txt | sha256sum -c - )
else
  ( cd "$TMP" && grep " $ASSET$" checksums.txt | shasum -a 256 -c - )
fi

if [ "$OS" = windows ]; then
  ( cd "$TMP" && unzip -o "$ASSET" "$BINARY" >/dev/null )
else
  tar -xzf "$TMP/$ASSET" -C "$TMP"
fi
[ -f "$TMP/$BINARY" ] || { echo "!! 解压后未找到 $BINARY" >&2; exit 1; }

# ---- 4. 安装 ----
install -d "$PREFIX"
install -m 0755 "$TMP/$BINARY" "$PREFIX/$BINARY"
echo "==> installed: $PREFIX/$BINARY"
"$PREFIX/$BINARY" --version

if ! case ":$PATH:" in *":$PREFIX:"*) ;; *) false ;; esac; then
  echo
  echo "!! $PREFIX 不在 PATH 中,请在 ~/.zshrc 添加:"
  echo "   export PATH=\"$PREFIX:\$PATH\""
fi