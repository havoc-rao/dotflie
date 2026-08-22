# dotf 与 mise 集成方案（本地 dev 版本管理）

本文件记录 dotf 接入 mise 的完整流程、方案取舍与踩坑记录（参考 shr 的同样方案）。
目标：让 **本地开发构建（`dist/dotf`）** 以 `dotf@dev` 的形式纳入 mise 管理，
随时切换、状态可查，且全程零网络依赖；正式版（GitHub Releases）作为 mise 的另一版本（走 `github:` 后端）。

---

## 1. 方案选型（为什么是"插件 + github 后端"双通道）

| 版本渠道 | 后端 | 说明 |
|---|---|---|
| dev（本地构建） | **asdf 兼容插件**（`mise plugins link` 本地目录） | 零网络，`list-all`/`install` 均本地执行 |
| release（发布版） | **`github:havoc-rao/dotflie`** | 仓库已有 GitHub Releases（`dotf update` 依赖它），版本列表可正常解析 |

为什么不全部用 `github:` 后端：

| 阻碍 | 说明 |
|---|---|
| dev 无法校验 | `github:` 后端每次操作会拉取 release 列表校验版本号；本地未发版的 `dev` 版本无法通过 |
| 网络不稳定 | 出口到 GitHub 连接时好时坏（SSL 超时），重试成功率约 1/3 |
| API 限流 | 未认证 GitHub API 每 IP 60 次/小时，共享出口 IP 极易 403 |

其他被否的方案：

- `mise link dotf@dev <path>`：官方样板（`node@brew`），但自定义工具 `dotf` 不在 mise registry，报 `not found in mise tool registry`；
- `file:` / `path:` 后端：mise 后端列表中不存在；
- `ubi:` 后端：官方已弃用，合并进 `github:` 后端。

**结论：dev 走本地 asdf 兼容插件（插件即代码，仓库自带）；正式版走 `github:` 后端。**
mise 完整实现了 asdf 插件协议，官方标注 asdf 为 legacy，但"本地构建产物映射为版本"这一场景完全够用。

---

## 2. 目录结构

插件本体位于本仓库（随项目走，多机 clone 即自带）：

```
scripts/mise/dotf/                ← asdf 插件（工具名 = 插件名 = dotf）
├── bin/
│   ├── list-all                  # 声明可用版本：echo "dev"
│   └── install                   # dev → symlink 到 dist/dotf（跟随最新构建）
```

mise 侧只有引用（可随时重建）：

```
~/.local/share/mise/plugins/dotf    → 本仓库 scripts/mise/dotf（symlink）
~/.local/share/mise/installs/dotf/dev/bin/dotf → dist/dotf（symlink，由 install 脚本创建）
```

install 脚本核心逻辑（`scripts/mise/dotf/bin/install`）：

```bash
case "$version" in
  dev)
    src="${DOTF_DEV_BIN:-$HOME/Documents/Projects/tools/dotfiles/dist/dotf}"
    [ -x "$src" ] || exit 1        # 要求先 make build
    ln -sf "$src" "$install_path/bin/dotf"   # symlink，构建后自动跟随
    ;;
esac
```

---

## 3. 接入步骤（新机器复现）

前置：已安装 mise 并在 shell 激活（`eval "$(mise activate zsh)"`）。

```bash
cd ~/Documents/Projects/tools/dotfiles
make build                                              # 确保 dist/dotf 存在

# ① 登记插件：把"dotf 这个名字的说明书"指到仓库
mise plugins link dotf "$(pwd)/scripts/mise/dotf"

# ② 注册 dev 版本：触发 list-all 确认 → install 建 symlink → 写配置 → 生成 shim
mise use -g --yes dotf@dev

# ③ 验证
mise ls                # dotf  dev  ~/.config/mise/config.toml  dev
mise which dotf | xargs readlink   # → .../dist/dotf（真身）
dotf -v                # dotf 0.1.0-dev (commit xxx, built 2026-08-22)
```

正式版（发版后，需网络）：

```bash
mise install dotf@0.1.0          # github:havoc-rao/dotflie 下载 release 归档
mise use -g dotf@0.1.0           # 切换（目录级：mise use dotf@0.1.0 写项目 .mise.toml）
mise alias set dotf prod 0.1.0   # 可选：版本别名更可读
```

产物一览：

| 环节 | 位置 |
|---|---|
| 插件登记 | `~/.local/share/mise/plugins/dotf` → 仓库 |
| 版本布局 | `~/.local/share/mise/installs/dotf/dev/bin/dotf` → `dist/dotf` |
| 激活配置 | `~/.config/mise/config.toml`：`[tools] dotf = "dev"` |
| shim | `~/.local/share/mise/shims/dotf`（PATH 中的入口） |

---

## 4. 日常使用

```bash
make build        # 改代码后构建：dist/dotf 更新，symlink 自动跟随，无需任何 mise 操作
dotf -v            # 确认当前版本（dev 构建带 -dev 后缀）
mise ls           # 状态查询
mise which dotf    # 查看解析路径（xargs readlink 看真身）
mise x dotf@dev -- dotf status   # 单次指定版本执行
```

版本切换（dev ↔ release）：

```bash
mise use -g dotf@dev        # 回到本地构建
mise use -g dotf@0.1.0      # 切到发布版
```

`dotf update` 在 dev 构建下不受影响：版本比较使用纯净的 `Version`（`0.1.0`），
若本地版本已不落后于最新 release 会提示 `already up to date` / `skipping`（跳过降级）。

---

## 5. 移除（完全退出 mise）

四个环节缺一不可，否则残留死链：

```bash
mise uninstall dotf@dev                     # ① 卸载版本（含 installs 布局）
sed -i '' '/^dotf = /d' ~/.config/mise/config.toml   # ② 删配置行
rm -f ~/.local/share/mise/shims/dotf        # ③ 删 shim（不会自动清理！）
mise plugins uninstall dotf                 # ④ 摘插件登记
```

---

## 6. 完整执行链路

```
输入 dotf
  → zsh 按 PATH 命中 ~/.local/share/mise/shims/dotf（shim，mise 在每个版本安装时生成）
  → shim 解析激活版本（config.toml: dotf = "dev"，目录级 .mise.toml 优先）
  → exec ~/.local/share/mise/installs/dotf/dev/bin/dotf（版本布局的固定入口）
  → 内核跟随 symlink → ~/Documents/Projects/tools/dotfiles/dist/dotf（真实二进制）
```

要点：**`installs/<tool>/<version>/bin/<binary>` 是 mise 与 shim 之间的契约路径，
必须存在；其内容可以是复制（快照）/symlink（跟随）/硬链接（跟随），
本项目选 symlink 以获得"构建即生效"。**

---

## 7. 踩坑记录

### shim 惰性

mise 的 shim 是"只生成、不更新"的：切换工具后端或卸载旧版本后，
已存在的 shim 不会自动重写，会变成指向已删除路径的死链：

```text
shims/dotf: line 2: .../installs/.../dev: No such file or directory
```

`mise reshim` 对已存在的 shim 也是惰性的（不覆盖）。**修复：删掉再生成**

```bash
rm ~/.local/share/mise/shims/dotf && mise reshim
```

### make 变量大小写

覆盖构建渠道要用大写：`make build CHANNEL=release`。
make 变量大小写敏感，小写 `channel=release` 是另一个变量，不会生效（`?=` 保持 dev）。

### 其他记忆点

- 插件脚本必须带可执行权限（`chmod +x`），否则 install 阶段报 Permission denied；
- `mise plugins link` 建议用绝对路径（相对路径依赖执行时 cwd）；
- 插件首次 link 后 `use` 需要 `--yes`（社区插件确认）。

---

## 8. 与其他安装方式的分工

| 层 | 工具 | 负责 |
|---|---|---|
| 版本选择（dotf） | **mise** | dev（本地构建）/ release（GitHub Releases）双版本、切换、状态查询 |
| 直装 | `make install` / `scripts/install.sh` | 不经版本管理，装到 `~/.local/bin`（`DOTFILES_PREFIX` 可覆盖） |
| Go 通道 | `go install github.com/havoc420/dotfiles@latest` | 有 Go 环境的临时通道 |
| 自更新 | `dotf update` | release 版原地升级；dev 版自动跳过降级 |

多机同步：`~/.config/mise/config.toml` 进 dotfiles（本项目自己就是干这个的）+ 本仓库自带 `scripts/mise/dotf`，
新机器两条命令（`plugins link` + `use -g`）即恢复 dev 通道。

---

## 9. 版本渠道标注（-dev 后缀）

`make build` 本地构建与 goreleaser 发布版通过 ldflags 注入的 `Channel` 区分：

| 构建来源 | Channel | `dotf -v` 显示 |
|---|---|---|
| `make build`（本地，`CHANNEL ?= dev`） | dev | `dotf 0.1.0-dev (...)` |
| `make build CHANNEL=release` | release | `dotf 0.1.0 (...)` |
| goreleaser snapshot | dev | `dotf 0.1.0-dev (...)` |
| goreleaser release | release | `dotf 0.1.0 (...)` |
| 裸 `go build`（无注入） | unknown | `dotf 0.1.0 (...)`（无后缀） |

- CLI 侧：`cli/cli.go` 的 `Channel` 变量 + version 命令拼接 `-dev`；`Version` 保持纯净（`dotf update` 版本比较不受影响）；
- Makefile / `scripts/build.sh`：`CHANNEL ?= dev` / `${CHANNEL:-dev}`，可用 `make build CHANNEL=release` 覆盖；
- `.goreleaser.yml`：`cli.Channel={{ if .IsSnapshot }}dev{{ else }}release{{ end }}`。