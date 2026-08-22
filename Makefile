# dotf Makefile — 统一构建入口，产物输出到 dist/
BINARY   := dotf
DIST     := dist
VERSION  := $(shell tr -d '[:space:]' < version/VERSION)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
# dev 构建时间含时分(本地时间),便于 -v 确认构建新鲜度;release 由 goreleaser 注入
BUILD_DATE := $(shell date "+%Y-%m-%d %H:%M")

# 构建渠道：dev = 本地开发构建（dotf -v 显示 0.1.0-dev）；release = 发布版（goreleaser 覆盖注入）。
# 覆盖示例: make build CHANNEL=release
CHANNEL  ?= dev

# 版本变量在 cli 包（cli.Commit / cli.Date / cli.Channel），main 委托 cli.Run
# Date 值含空格,需引号包裹(-ldflags 解析支持 shell 式引号)
LDFLAGS  := -s -w \
	-X github.com/havoc420/dotfiles/cli.Commit=$(GIT_COMMIT) \
	-X 'github.com/havoc420/dotfiles/cli.Date=$(BUILD_DATE)' \
	-X github.com/havoc420/dotfiles/cli.Channel=$(CHANNEL)

.PHONY: build run release snapshot install test clean help

## build: 编译当前平台二进制到 dist/dotf
build:
	mkdir -p $(DIST)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) .

## run: 编译并立即运行（透传参数: make run args="status"）
run: build
	./$(DIST)/$(BINARY) $(args)

## release: 发版。用法:
##   make release          自动 patch+1 (0.1.0 → 0.1.1)
##   make release v=0.2.0  指定版本号 (升级 minor/major 时用)
##   写入 version/VERSION → commit → push，CI 自动打 tag 并跑 goreleaser
release:
	@cur=$$(tr -d '[:space:]' < version/VERSION); \
	if [ -n "$(v)" ]; then new="$(v)"; \
	else \
		major=$$(echo $$cur | cut -d. -f1); \
		minor=$$(echo $$cur | cut -d. -f2); \
		patch=$$(echo $$cur | cut -d. -f3); \
		patch=$$((patch + 1)); \
		new="$$major.$$minor.$$patch"; \
	fi; \
	echo "$$new" > version/VERSION; \
	git add version/VERSION; \
	git commit -m "release: v$$new"; \
	git push origin $$(git rev-parse --abbrev-ref HEAD); \
	echo "✓ v$$cur → v$$new 已推送，CI 将自动打 tag 并发布 Release"

## snapshot: 本地单平台 goreleaser 快照（不发版，产物在 dist/；多平台交叉构建只在 GitHub CI 跑）
snapshot:
	goreleaser release --snapshot --clean --single-target

## install: 编译并安装到 ~/.local/bin（可 DOTFILES_PREFIX 覆盖）
install: build
	install -d $(DOTFILES_PREFIX) 2>/dev/null || true
	@PREFIX=$${DOTFILES_PREFIX:-$$HOME/.local/bin}; \
	install -d "$$PREFIX"; \
	install -m 0755 $(DIST)/$(BINARY) "$$PREFIX/$(BINARY)"; \
	echo "✓ 已安装到 $$PREFIX/$(BINARY)"

## test: 运行全部测试（禁用缓存避免误报）
test:
	go test ./... -count=1

## clean: 清理 dist/
clean:
	rm -rf $(DIST)

## help: 显示本帮助
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //; s/: /\t/'
