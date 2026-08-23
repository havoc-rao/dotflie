// init/list/status/link/unlink 命令与 TUI 交互（原 main.go 命令层）。
package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/havoc420/dotfiles/tui"
)

// ---- init ----

func cmdInit(args []string) error {
	force := false
	parseFlags(args, map[string]*bool{"--force": &force}, nil)
	path := DefaultNames[0]
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force)", path)
	}
	if err := os.WriteFile(path, []byte(DefaultTemplate), 0o644); err != nil {
		return err
	}
	// 生成本机路径文件的示例副本,并把私有文件加入 .gitignore
	_ = os.WriteFile(envExampleFile(), []byte(envExampleContent), 0o644)
	_ = ensureGitIgnore()
	// 记录仓库根:之后任意目录可直接运行 dotf 命令,无需 cd
	if abs, err := filepath.Abs("."); err == nil {
		if err := SaveRoot(filepath.Clean(abs)); err == nil {
			fmt.Printf("%s %s %s\n", okTag("root 已记录:", 0), info(filepath.Clean(abs)), dim("(任意目录可直接运行 dotf 命令)"))
		}
	}
	return nil
}

// envExampleFile 是本机路径示例文件名(= .dotfiles.env.example)。
func envExampleFile() string {
	return envFile + ".example"
}

const envExampleContent = `# dotfiles 本机路径示例:复制为 ` + "`.dotfiles.env`" + ` 后填写本机真实路径
#   cp .dotfiles.env.example .dotfiles.env
# 该文件已由 dotf init 加入 .gitignore,不随仓库提交;键名与清单中 {key} 一致。
#projects=/Users/you/projects
#space_labeler=/Users/you/Projects/tools/macOS/space-labeler
`

// ensureGitIgnore 把 .dotfiles.env 追加进 .gitignore(已包含则跳过)。
func ensureGitIgnore() error {
	data, err := os.ReadFile(".gitignore")
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if bytes.Contains(data, []byte(envFile)) {
		return nil
	}
	var b strings.Builder
	b.Write(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "\n# dotf 本机路径(私有)\n%s\n", envFile)
	return os.WriteFile(".gitignore", []byte(b.String()), 0o644)
}

// ---- list / status ----

func cmdList(args []string) error {
	asJSON := false
	parseFlags(args, map[string]*bool{"--json": &asJSON}, nil)
	m, root, err := load()
	if err != nil {
		return err
	}
	entries, err := Collect(m, root, nil)
	if err != nil {
		return err
	}
	if asJSON {
		out := make([]map[string]string, 0, len(entries))
		for _, e := range entries {
			out = append(out, map[string]string{"src": e.Link.Src, "dest": e.DestAbs})
		}
		return printJSON(out)
	}
	for _, e := range entries {
		fmt.Printf("%s  %s\n", pad(e.Link.Src, 28), e.DestAbs)
	}
	return nil
}

func cmdStatus(args []string) error {
	asJSON := false
	parseFlags(args, map[string]*bool{"--json": &asJSON}, nil)
	m, root, err := load()
	if err != nil {
		return err
	}
	entries, err := Collect(m, root, nil)
	if err != nil {
		return err
	}
	if asJSON {
		out := make([]map[string]string, 0, len(entries))
		for _, e := range entries {
			out = append(out, map[string]string{"status": e.Status.String(), "src": e.Link.Src, "dest": e.DestAbs})
		}
		return printJSON(out)
	}
	for _, e := range entries {
		fmt.Println(e.Describe())
	}
	return nil
}

// ---- link / unlink ----

func cmdLink(args []string, unlink bool) error {
	o := Options{}
	all := false
	targets := parseFlags(args, map[string]*bool{
		"--dry-run": &o.DryRun,
		"--force":   &o.Force,
		"--backup":  &o.Backup,
		"--quiet":   &o.Quiet,
		"--all":     &all,
	}, nil)
	// unlink 不区分 force/backup
	if unlink {
		o.Force, o.Backup = false, false
	}
	m, root, err := load()
	if err != nil {
		return err
	}
	// 终端交互:无目标参数且未指定 --all 时进入 TUI 多选
	if len(targets) == 0 && !all && isTTY() {
		return linkInteractive(m, root, o, unlink)
	}
	entries, err := Collect(m, root, targets)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no matching links")
	}
	return applyLinks(entries, o, unlink)
}

// applyLinks 逐个执行链接/解除并汇总错误。
func applyLinks(entries []Entry, o Options, unlink bool) error {
	failed := 0
	for _, e := range entries {
		var err error
		if unlink {
			err = unlinkEntry(e, o)
		} else {
			err = linkEntry(e, o)
		}
		if err != nil {
			failed++
			if o.Quiet {
				fmt.Fprintf(os.Stderr, "%s %s\n", eFail("dotf:", 0), err)
			} else {
				fmt.Printf("%s %s  ->  %s\n", failTag("ERROR", 22), e.Link.Src, e.DestAbs)
				fmt.Fprintf(os.Stderr, "%s\n", eErr("  "+err.Error()))
			}
		} else if !o.Quiet && !o.DryRun {
			verb := "linked"
			if unlink {
				verb = "unlinked"
			}
			fmt.Printf("%s %s\n", step(verb, 22), e.DestAbs)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d failed", failed)
	}
	return nil
}

// linkInteractive 用 TUI 多选待处理条目,回车后执行。
// 默认勾选:link 勾未链接项;unlink 勾已链接/失效链接项。
func linkInteractive(m *Manifest, root string, o Options, unlink bool) error {
	entries, err := Collect(m, root, nil)
	if err != nil {
		return err
	}
	verb := "link"
	if unlink {
		verb = "unlink"
	}
	cands := make([]tui.Candidate, 0, len(entries))
	var chosen []string
	for _, e := range entries {
		cands = append(cands, tui.Candidate{Value: e.Link.Src, Desc: e.DestAbs})
		if !unlink && e.Status == StatusNotLinked {
			chosen = append(chosen, e.Link.Src)
		}
		if unlink && (e.Status == StatusLinked || e.Status == StatusStale) {
			chosen = append(chosen, e.Link.Src)
		}
	}
	in, out, err := openTTY()
	if err != nil {
		return err
	}
	defer closeTTY(in, out)
	res, err := tui.Run(tui.Options{
		Title:      "dotf " + verb,
		Subtitle:   "space 勾选 · enter 执行 · esc 取消",
		Cands:      cands,
		Multi:      true,
		Selected:   chosen,
		StatusHint: "输入过滤 · j/k 移动 · space 勾选 · enter 确认",
	}, in, out)
	if err != nil {
		return err
	}
	if res.Cancelled {
		fmt.Println(info("已取消"))
		return nil
	}
	if len(res.Selected) == 0 {
		fmt.Println(dim("未选择任何条目"))
		return nil
	}
	// 勾选的 src 对应到 Entry
	var picked []Entry
	want := map[string]bool{}
	for _, s := range res.Selected {
		want[s] = true
	}
	for _, e := range entries {
		if want[e.Link.Src] {
			picked = append(picked, e)
		}
	}
	return applyLinks(picked, o, unlink)
}
