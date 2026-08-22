// Package cli 实现 dotf 的全部命令与核心逻辑（参考 shr 的 cli 分层）。
//
// 目录结构:
//
//	cli.go      命令路由 + 公共辅助（load/parseFlags/isTTY/usage 等）
//	commands.go init/list/status/link/unlink 命令 + TUI 交互
//	manifest.go 清单解析 + 向上查找 + ~/$ENV 展开
//	link.go     符号链接的创建/移除/状态机
//	update.go   GitHub Releases 自更新
package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/havoc420/dotfiles/version"
)

// 版本信息：Version 来自 version/VERSION 文件（go:embed 嵌入），
// Commit/Date/Channel 由构建方注入（Makefile 本地构建 → Channel=dev；goreleaser 发布版 → Channel=release）。
var (
	Version = version.Version
	Commit  = "none"
	Date    = "unknown"
	Channel = "unknown" // 构建渠道：dev（本地构建）| release（发布版），ldflags -X 注入
)

// Run 执行 CLI 入口，返回 error（main 负责打印与退出码）。
func Run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "init":
		return cmdInit(rest)
	case "list":
		return cmdList(rest)
	case "status":
		return cmdStatus(rest)
	case "link":
		return cmdLink(rest, false)
	case "unlink":
		return cmdLink(rest, true)
	case "update":
		return cmdUpdate(rest)
	case "version", "-v", "--version":
		// dev 渠道的构建在版本号后附加 -dev 后缀，便于与发布版区分（如 mise 双版本管理）。
		v := Version
		if Channel == "dev" {
			v += "-dev"
		}
		fmt.Printf("dotf %s (commit %s, built %s)\n", v, Commit, Date)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q (try: dotf help)", cmd)
	}
}

// load 向上查找 manifest 并加载。
func load() (*Manifest, string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	mpath, root, err := Find(wd)
	if err != nil {
		return nil, "", err
	}
	m, err := Load(mpath)
	if err != nil {
		return nil, "", err
	}
	return m, root, nil
}

// parseFlags 从 args 中提取以 - 开头的 flag(布尔)与值 flag,返回剩余位置参数。
func parseFlags(args []string, booleans map[string]*bool, values map[string]*string) []string {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 1 && a[0] == '-' {
			name := a
			// --key=value
			key, val, hasEq := splitFlag(a)
			if b, ok := booleans[key]; ok {
				if hasEq {
					*b = val == "true" || val == "1"
				} else {
					*b = true
				}
				continue
			}
			if v, ok := values[key]; ok {
				if hasEq {
					*v = val
				} else if i+1 < len(args) {
					i++
					*v = args[i]
				}
				continue
			}
			fmt.Fprintf(os.Stderr, "dotf: ignoring unknown flag %s\n", name)
			continue
		}
		pos = append(pos, a)
	}
	return pos
}

func splitFlag(a string) (key, val string, hasEq bool) {
	if i := indexByte(a, '='); i >= 0 {
		return a[:i], a[i+1:], true
	}
	return a, "", false
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// isTTY 判断 stdin 是否为交互终端。
func isTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// openTTY 打开控制终端,供 TUI 渲染与输入使用。
// 优先 /dev/tty(独立于 stdout);不可用时回退 stdin/stdout。
func openTTY() (in, out *os.File, err error) {
	if f, e := os.OpenFile("/dev/tty", os.O_RDWR, 0); e == nil {
		return f, f, nil
	}
	return os.Stdin, os.Stdout, nil
}

// closeTTY 关闭 openTTY 打开的文件(回退到 stdin/stdout 时不关闭)。
func closeTTY(in, out *os.File) {
	if in != nil && in != os.Stdin {
		_ = in.Close()
	}
	if out != nil && out != os.Stdout && out != in {
		_ = out.Close()
	}
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func pad(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}

func usage() {
	fmt.Print(`dotf - 个人配置文件管理工具

用法:
  dotf init              在当前目录生成 .dotfiles.yaml 模板
  dotf list              列出所有映射配置
  dotf status            显示每个映射的链接状态
  dotf link [目标...]    建立符号链接 (目标可匹配 src/dest 名称)
  dotf unlink [目标...]  移除符号链接
  dotf update [版本]     从 GitHub Releases 自更新 (--check 仅检查)

交互:
  在终端中直接运行 link/unlink(不带目标参数)会进入 TUI 多选:
  实时过滤 · j/k 移动 · space 勾选 · enter 执行 · esc 取消
  加 --all 跳过交互直接处理全部;管道/脚本调用时也自动全量。

link/unlink 选项:
  --all       跳过交互,处理全部条目
  --dry-run   只打印将要执行的操作
  --force     替换指向别处的旧符号链接
  --backup    目标为真实文件时先备份为 .dotfiles.bak.<时间戳>
  --quiet     静默模式

示例:
  dotf link           # TUI 勾选要链接的条目(或 --all 全量)
  dotf link .zshrc    # 只链接 .zshrc
  dotf link --dry-run # 预览
  dotf status --json  # 机器可读状态
`)
}
