# dotfiles

个人配置文件管理工具。通过一份 `.dotfiles.yaml` 清单，把仓库内的文件/目录以符号链接的方式部署到目标位置，支持 TUI 交互式勾选、状态检查与一键回滚。

## 特性

- **YAML 清单驱动**：`src -> dest` 显式映射，一目了然
- **TUI 交互**（风格参考 shr/tui）：实时过滤、多选勾选、回车执行；脚本/管道环境自动退化为全量模式
- **目录整链接**：目录条目整体符号链接，适合整目录同步（如 `~/.config/nvim`）
- **幂等安全**：已正确链接自动跳过；冲突自动识别并提示，`--backup` 可备份后替换
- **多平台路径**：`dest` 支持 `~` 与 `$ENV` 展开
- **机器可读输出**：`--json` 便于接入其他工具

## 安装

```sh
make install        # 构建并安装到 ~/.local/bin (DOTFILES_PREFIX 可覆盖)
./scripts/install.sh # 等价脚本
```

## 版本与发版

版本真相源为 `version/VERSION`，编译时通过 `go:embed` 静态嵌入，`dotf --version` 可查看。

```sh
make release           # 自动 patch+1 (0.1.0 → 0.1.1)
make release v=0.2.0   # 指定版本号 (升级 minor/major 时用)
make snapshot          # 本地跑 goreleaser 快照构建(不发版,产物在 dist/)
# 写入 version/VERSION → commit → push → CI(.github/workflows/release.yml)
# 自动打 tag 并跑 goreleaser 交叉编译发布到 GitHub Releases。
```

已安装后可直接自更新：

```sh
dotf update            # 从 GitHub Releases 自更新到最新版(原地替换二进制)
dotf update --check    # 仅检查是否有新版本
dotf update 0.2.0      # 更新到指定版本
```

## 快速开始

```sh
dotf init          # 生成 .dotfiles.yaml 模板
# 编辑清单,把仓库内文件映射到目标位置
dotf link          # TUI 勾选要链接的条目,回车执行
dotf status        # 查看各条目链接状态
```

## 命令

| 命令 | 说明 |
| --- | --- |
| `dotf init [--force]` | 生成 `.dotfiles.yaml` 模板 |
| `dotf list [--json]` | 列出所有映射配置 |
| `dotf status [--json]` | 显示每个映射的链接状态 |
| `dotf link [目标...]` | 建立符号链接 |
| `dotf unlink [目标...]` | 移除符号链接 |
| `dotf update [版本] [--check]` | 从 GitHub Releases 自更新到最新（或指定）版本 |

> `link`/`unlink` 带目标参数时只处理匹配的条目（可按 src/dest 名称匹配）；不带参数时：
> - 终端中 → 进入 TUI 多选
> - 非终端（管道/脚本）→ 全量处理
> - 加 `--all` → 跳过交互直接全量

### link/unlink 选项

| 选项 | 说明 |
| --- | --- |
| `--all` | 跳过交互，处理全部条目 |
| `--dry-run` | 只打印将要执行的操作 |
| `--force` | 替换指向别处的旧符号链接 |
| `--backup` | 目标为真实文件时先备份为 `.dotfiles.bak.<时间戳>` |
| `--quiet` | 静默模式 |

## TUI 交互

在终端中直接运行 `dotf link` / `dotf unlink` 进入选择器：

```
dotf link  ·  space 勾选 · enter 执行 · esc 取消
────────────────────────────────────────────────────
  ▸ [x] zsh/.zshrc      /Users/you/.zshrc
    [ ] git/.gitconfig  /Users/you/.gitconfig
    [ ] nvim            /Users/you/.config/nvim
────────────────────────────────────────────────────
❯ ▏
  3/3  ·  输入过滤 · j/k 移动 · space 勾选 · enter 确认
```

- 输入即时过滤（空格分隔多 token，大小写不敏感），命中片段高亮
- `j`/`k` 或 `↑`/`↓` 移动，`space` 勾选，`enter` 执行，`esc`/`ctrl+c` 取消
- `ctrl+u` 清空输入，`ctrl+w` 删除上一个词
- `link` 默认勾选未链接项，`unlink` 默认勾选已链接项

## 清单格式

`.dotfiles.yaml` 位于仓库根目录（支持 `dotfiles.yaml` 等候选名，运行时可从任意子目录向上查找）：

```yaml
# src 相对本仓库根目录; dest 为目标绝对路径(支持 ~ 与 $ENV)
# 目录条目整体符号链接(不展开内容),便于整目录同步。
links:
  - src: zsh/.zshrc
    dest: ~/.zshrc
  - src: git/.gitconfig
    dest: ~/.gitconfig
  - src: nvim
    dest: ~/.config/nvim
```

## 状态说明

| 状态 | 含义 |
| --- | --- |
| `linked` | 已正确链接 |
| `not-linked` | 尚未创建链接 |
| `missing-src` | 源文件/目录不存在 |
| `conflict` | 目标位置是真实文件（非符号链接） |
| `stale` | 目标是指向别处的符号链接 |

## 目录结构

```
dotfiles/
├── main.go                      # 薄入口:委托 cli.Run(os.Args[1:])
├── cli/                         # 全部命令与核心逻辑(参考 shr 分层)
│   ├── cli.go                   # 命令路由 + 公共辅助(load/parseFlags/isTTY/usage)
│   ├── commands.go              # init/list/status/link/unlink 命令 + TUI 交互
│   ├── manifest.go              # 清单解析 + 向上查找 + ~/$ENV 展开
│   ├── link.go                  # 链接/解除/状态机
│   └── update.go                # dotf update:GitHub Releases 自更新(标准库实现)
├── tui/tui.go                   # 过滤选择器 TUI(bubbletea + lipgloss,参考 shr/tui)
├── .goreleaser.yml              # 交叉编译发布配置(linux/darwin/windows × amd64/arm64)
├── .github/workflows/release.yml # push version/VERSION 自动打 tag + goreleaser 发版
└── README.md
```

## 依赖

- [bubbletea](https://github.com/charmbracelet/bubbletea) — TUI 框架
- [lipgloss](https://github.com/charmbracelet/lipgloss) — 样式渲染
- [termenv](https://github.com/muesli/termenv) — 终端颜色探测
- [yaml.v3](https://gopkg.in/yaml.v3) — 清单解析
