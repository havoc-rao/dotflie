// dotf commit 子命令:版本化提交(dotfiles 仓库演进版本,semver 三段 0.0.1 起步)。
// 版本真相源为仓库根/version 文件(内容 "X.Y.Z",随仓库 git 跟踪),
// 默认步长 patch;提交自动打 tag vX.Y.Z(--no-tag 可关)。
// commit 标题携带版本号,正文附变更明细。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// versionFile 是仓库版本文件(内容如 "0.0.1",semver 三段)。
const versionFile = "version"

// semver 三段版本号。
type semver struct{ major, minor, patch int }

func (v semver) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

// Usage:
//
//	dotf commit [--minor|--major|--no-bump] [--no-tag] [消息...]
//	dotf commit --status
func cmdCommit(args []string) error {
	status := false
	minor := false
	major := false
	noBump := false
	noTag := false
	rest := parseFlags(args, map[string]*bool{
		"--status": &status, "--minor": &minor, "--major": &major, "--no-bump": &noBump, "--no-tag": &noTag,
	}, nil)
	if status {
		return commitStatus()
	}
	want := 0
	if minor {
		want++
	}
	if major {
		want++
	}
	if want > 1 {
		return fmt.Errorf("--minor/--major 互斥,只能选一个")
	}
	msg := strings.Join(rest, " ")
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	_, root, err := Find(wd)
	if err != nil {
		return fmt.Errorf("no manifest found (run dotf init first)")
	}
	// 1) 前置检查:有未提交变更才提交
	out, err := gitRun(root, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return fmt.Errorf("nothing to commit (工作区干净)")
	}
	// 2) 版本步进:默认 patch+1;--minor 升 minor 清 patch;--major 升 major 清小位
	vp := filepath.Join(root, versionFile)
	ver, err := readVersion(vp)
	if err != nil {
		return err
	}
	old := ver.String()
	if !noBump {
		switch {
		case major:
			ver.major++
			ver.minor, ver.patch = 0, 0
		case minor:
			ver.minor++
			ver.patch = 0
		default:
			ver.patch++
		}
	}
	new := ver.String()
	// 3) 先记旧值,写新版本;提交失败回滚
	if err := os.WriteFile(vp, []byte(new+"\n"), 0o644); err != nil {
		return err
	}
	rollback := func() {
		_ = os.WriteFile(vp, []byte(old+"\n"), 0o644)
	}
	// 4) 构造提交信息:标题=版本+消息,正文=变更明细
	title := "v" + new + ": " + summaryOf(msg, len(lines))
	body := diffBody(lines)
	if _, err := gitRun(root, "add", "-A"); err != nil {
		rollback()
		return fmt.Errorf("git add: %w", err)
	}
	if _, err := gitRun(root, "commit", "-m", title, "-m", body); err != nil {
		rollback()
		return fmt.Errorf("git commit: %w", err)
	}
	// 5) 自动打 tag(版本与 tag 一一对应;--no-tag 关闭;同名 tag 已存在则跳过不报错)
	tagged := false
	if !noTag {
		if _, err := gitRun(root, "tag", "v"+new); err != nil {
			fmt.Fprintf(os.Stderr, "%s tag v%s 未创建(%v)\n", eWarn("dotf: warning:"), new, err)
		} else {
			tagged = true
		}
	}
	// 6) 结果摘要(version 文件本身也在变更里,故 +1)
	hash, _ := gitRun(root, "rev-parse", "--short", "HEAD")
	fmt.Printf("%s → %s (%d files) %s: %s", info("v"+old), info("v"+new), len(lines)+1, okTag("committed", 0), hash)
	if tagged {
		fmt.Printf(" (%s)", info("tag v"+new))
	}
	fmt.Println()
	return nil
}

// commitStatus 只读:当前版本 + 待提交变更概览,不做任何修改。
func commitStatus() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	_, root, err := Find(wd)
	if err != nil {
		return fmt.Errorf("no manifest found (run dotf init first)")
	}
	vp := filepath.Join(root, versionFile)
	ver, err := readVersion(vp)
	if err != nil {
		return err
	}
	fmt.Printf("%s %s\n", field("version:"), info("v"+ver.String()))
	out, _ := gitRun(root, "status", "--porcelain")
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		fmt.Printf("%s  %s\n", field("changes:"), okTag("none (工作区干净)", 0))
		return nil
	}
	fmt.Printf("%s  %s\n", field("changes:"), warn(fmt.Sprintf("%d 项待提交", len(lines))))
	for _, l := range lines {
		fmt.Printf("          %s\n", l)
	}
	return nil
}

// readVersion 读取版本文件为 semver(文件缺失/未初始化时返回 0.0.0,首次提交即 0.0.1)。
func readVersion(p string) (semver, error) {
	v := semver{}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return v, nil
		}
		return v, err
	}
	s := strings.TrimSpace(string(data))
	parts := strings.Split(s, ".")
	nums := make([]int, 0, 3)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return v, fmt.Errorf("parse %s: invalid version %q (expected X.Y.Z)", p, s)
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return v, fmt.Errorf("parse %s: invalid version %q", p, s)
		}
		nums = append(nums, n)
	}
	if len(nums) >= 1 {
		v.major = nums[0]
	}
	if len(nums) >= 2 {
		v.minor = nums[1]
	}
	if len(nums) >= 3 {
		v.patch = nums[2]
	}
	if len(nums) > 3 {
		return v, fmt.Errorf("parse %s: invalid version %q (expected X.Y.Z)", p, s)
	}
	if v.major < 0 || v.minor < 0 || v.patch < 0 {
		return v, fmt.Errorf("parse %s: invalid version %q", p, s)
	}
	return v, nil
}

// summaryOf 提交标题摘要:有用户消息用之,否则自动摘要。
func summaryOf(msg string, n int) string {
	if msg != "" {
		return msg
	}
	return fmt.Sprintf("dotfiles 配置更新(%d files)", n)
}

// diffBody 生成变更明细正文(状态 + 路径,最多 20 条,超出截断)。
func diffBody(lines []string) string {
	const max = 20
	var b strings.Builder
	total := len(lines)
	if total > max {
		lines = lines[:max]
	}
	for _, l := range lines {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	if total > max {
		fmt.Fprintf(&b, "- ... (%d more)\n", total-max)
	}
	return strings.TrimSpace(b.String())
}
