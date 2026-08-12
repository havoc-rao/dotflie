#!/usr/bin/env bash
# 构建 dotf 二进制到 bin/dotf。
# 版本号来自 version/VERSION(唯一真相源,go:embed 嵌入),并注入 git commit 与构建日期。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="$(tr -d '[:space:]' < version/VERSION)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
DATE="$(date -u +%Y-%m-%d)"

echo "==> building dotf v${VERSION} (commit ${COMMIT}, ${DATE})"
mkdir -p bin
go build -trimpath -ldflags "-s -w -X github.com/havoc420/dotfiles/cli.Commit=${COMMIT} -X github.com/havoc420/dotfiles/cli.Date=${DATE}" -o bin/dotf .
echo "==> done: bin/dotf"
