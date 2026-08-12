// dotfiles: 个人配置文件管理工具。
// 通过 .dotfiles.yaml 清单把仓库内的文件/目录符号链接到目标位置。
// 全部命令与逻辑在 cli 包（参考 shr 分层）。
package main

import (
	"fmt"
	"os"

	"github.com/havoc420/dotfiles/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "dotf:", err)
		os.Exit(1)
	}
}
