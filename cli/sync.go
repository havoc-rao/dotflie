// dotf sync 子命令:输出 git remote 与本地同步状态(本地未提交变更、remote 是否有新更新)。
package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Usage: dotf sync [--no-fetch]
func cmdSync(args []string) error {
	noFetch := false
	parseFlags(args, map[string]*bool{"--no-fetch": &noFetch}, nil)
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	_, root, err := Find(wd)
	if err != nil {
		return fmt.Errorf("no manifest found (run dotf init first)")
	}
	fmt.Println(info("git sync:"))
	// remote
	url, err := gitRun(root, "remote", "get-url", "origin")
	if err != nil {
		fmt.Printf("  %s  %s\n", field("remote:"), warn("(none) — 仓库未配置 git remote,无法同步"))
		return nil
	}
	fmt.Printf("  %s  %s\n", field("remote:"), url)
	// branch / upstream
	branch, _ := gitRun(root, "branch", "--show-current")
	upstream, uperr := gitRun(root, "rev-parse", "--abbrev-ref", "@{u}")
	switch {
	case uperr == nil && branch != "":
		fmt.Printf("  %s  %s ⇄ %s\n", field("branch:"), branch, upstream)
	case branch != "":
		fmt.Printf("  %s  %s (%s)\n", field("branch:"), branch, warn("未设置 upstream,首次推送: git push -u origin "+branch))
	default:
		fmt.Printf("  %s  %s\n", field("branch:"), dim("(detached HEAD)"))
	}
	// local changes
	if out, err := gitRun(root, "status", "--porcelain"); err == nil && strings.TrimSpace(out) != "" {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		fmt.Printf("  %s  %s\n", field("local:"), warn(fmt.Sprintf("%d 个未提交变更", len(lines))))
		for _, l := range lines {
			fmt.Printf("            %s\n", strings.TrimSpace(l))
		}
	} else {
		fmt.Printf("  %s  %s\n", field("local:"), okTag("clean (无未提交变更)", 0))
	}
	// fetch(只要有 remote 就执行,与 upstream 无关):失败必须明确提示,不静默
	fetchOK := noFetch
	if !noFetch {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", "fetch", "origin")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				fmt.Printf("  %s  %s (30s timeout) — %s\n", field("fetch:"), failTag("FAILED", 0), "无法检查 remote 更新")
			} else {
				msg := strings.TrimSpace(string(out))
				if msg == "" {
					msg = err.Error()
				}
				fmt.Printf("  %s  %s — %s\n", field("fetch:"), failTag("FAILED", 0), msg)
			}
			fmt.Printf("  %s  %s\n", field("remote:"), warn("unknown (fetch 失败,remote 状态不可信;可稍后重试 dotf sync)"))
		} else {
			fetchOK = true
			fmt.Printf("  %s  %s\n", field("fetch:"), okTag("ok", 0))
		}
	} else if noFetch {
		fmt.Printf("  %s  %s\n", field("fetch:"), step("skipped (--no-fetch,remote 状态基于本地已有 refs)", 0))
	}
	// ahead / behind(基于最新 refs:fetch 成功或 --no-fetch 时的本地 refs)
	if uperr == nil && fetchOK {
		res, _ := gitRun(root, "rev-list", "--left-right", "--count", upstream+"...HEAD")
		parts := strings.Fields(res)
		if len(parts) == 2 {
			behind, ahead := parts[0], parts[1]
			switch {
			case behind != "0" && ahead != "0":
				fmt.Printf("  %s  %s提交(%s)\n", field("remote:"), warn(fmt.Sprintf("behind %s / ahead %s", behind, ahead)), info("建议: 先 git pull 再 git push"))
			case behind != "0":
				fmt.Printf("  %s  %s提交(%s)\n", field("remote:"), warn(fmt.Sprintf("behind %s", behind)), info("建议: git pull"))
			case ahead != "0":
				fmt.Printf("  %s  %s提交(%s)\n", field("remote:"), warn(fmt.Sprintf("ahead %s", ahead)), info("建议: git push"))
			default:
				fmt.Printf("  %s  %s\n", field("remote:"), okTag("up to date (与 remote 一致)", 0))
			}
		}
	}
	return nil
}
