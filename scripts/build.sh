#!/usr/bin/env bash
# 构建 dotf 二进制到 dist/dotf。
# 版本号来自 version/VERSION(唯一真相源,go:embed 嵌入),并注入 git commit、构建日期与构建渠道。
# 渠道: dev(默认,本地构建,dotf -v 显示 0.1.0-dev)| release(发布版,可 CHANNEL=release 覆盖)。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="$(tr -d '[:space:]' < version/VERSION)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
DATE="$(date -u +%Y-%m-%d)"
CHANNEL="${CHANNEL:-dev}"

echo "==> building dotf v${VERSION} (${CHANNEL}, commit ${COMMIT}, ${DATE})"
mkdir -p dist
go build -trimpath -ldflags "-s -w \
  -X github.com/havoc420/dotfiles/cli.Commit=${COMMIT} \
  -X github.com/havoc420/dotfiles/cli.Date=${DATE} \
  -X github.com/havoc420/dotfiles/cli.Channel=${CHANNEL}" -o dist/dotf .
echo "==> done: dist/dotf"
