// 符号链接的创建/移除/状态检查（原 internal/link，整合进 cli 包）。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status 表示一个链接项的当前状态。
type Status int

const (
	// StatusLinked 已正确链接。
	StatusLinked Status = iota
	// StatusMissingSrc 源文件/目录不存在。
	StatusMissingSrc
	// StatusConflict dest 已存在且不是符号链接(真实文件)。
	StatusConflict
	// StatusStale dest 是指向别处的符号链接。
	StatusStale
	// StatusNotLinked 尚未创建链接。
	StatusNotLinked
	// StatusRefUnset dest 引用了未设置的 {paths.key}。
	StatusRefUnset
)

func (s Status) String() string {
	switch s {
	case StatusLinked:
		return "linked"
	case StatusMissingSrc:
		return "missing-src"
	case StatusConflict:
		return "conflict"
	case StatusStale:
		return "stale"
	case StatusRefUnset:
		return "ref-unset"
	default:
		return "not-linked"
	}
}

// Entry 是待处理的一个链接项。
type Entry struct {
	Link    LinkSpec
	SrcAbs  string
	DestAbs string
	Status  Status
	Message string
	// ResolveErr 记录 dest 展开失败(如 {key} 未设置):条目仍保留在结果中,
	// 由 list/status 展示、link 时按条报错,不阻塞其它条目。
	ResolveErr error
}

// Options 控制链接行为。
type Options struct {
	DryRun bool // 只打印将要执行的操作
	Force  bool // 替换指向别处的符号链接
	Backup bool // 冲突时把现有文件备份为 .dotfiles.bak.<ts>
	Quiet  bool
}

// Collect 解析 manifest 并组装 Entry 列表。targets 为空表示全部。
// 处理顺序:先按机器过滤(only/except),再按 targets 过滤,最后解析 dest——
// 无关条目的 {key} 未设置不会阻塞定向操作;未解析的条目以 ResolveErr 记录,不中断。
func Collect(m *Manifest, repoRoot string, targets []string) ([]Entry, error) {
	var out []Entry
	for _, l := range m.Links {
		ok, err := l.LinkAppliesToHost(HostTag())
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if len(targets) > 0 && !matchTarget(l, targets) {
			continue
		}
		e := Entry{Link: l, SrcAbs: m.SrcAbs(repoRoot, l.Src)}
		dest, err := m.DestAbs(l.Dest)
		if err != nil {
			// 未设置 {key}:默认忽略该条目(不链接、全量操作不报错)。
			// Message 记录缺失的 key,便于展示;ResolveErr 保留完整提示供显式操作报错。
			e.ResolveErr = err
			e.Message = strings.Join(unsetRefKeys(l.Dest, m.Paths), ", ")
			e.Status = StatusRefUnset
			out = append(out, e)
			continue
		}
		e.DestAbs = dest
		e.Status = statusOf(e)
		out = append(out, e)
	}
	return out, nil
}

// matchTarget 判断条目是否命中某个目标参数(按 src 名 / src / dest 原始串匹配)。
func matchTarget(l LinkSpec, targets []string) bool {
	name := filepath.Base(l.Src)
	for _, t := range targets {
		if t == name || t == l.Src || t == l.Dest {
			return true
		}
	}
	return false
}

// statusOf 判断单个 Entry 的状态。
func statusOf(e Entry) Status {
	if _, err := os.Lstat(e.SrcAbs); err != nil {
		return StatusMissingSrc
	}
	fi, err := os.Lstat(e.DestAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return StatusNotLinked
		}
		return StatusConflict
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return StatusConflict
	}
	target, _ := os.Readlink(e.DestAbs)
	if !pathsEqual(target, e.SrcAbs) {
		return StatusStale
	}
	return StatusLinked
}

// linkEntry 为单个 Entry 建立符号链接。
func linkEntry(e Entry, o Options) error {
	if e.ResolveErr != nil {
		return e.ResolveErr
	}
	status := statusOf(e)
	if status == StatusLinked {
		return nil // 已就绪
	}
	if status == StatusMissingSrc {
		return fmt.Errorf("source not found: %s", e.SrcAbs)
	}
	action := "link " + e.SrcAbs + " -> " + e.DestAbs

	switch status {
	case StatusNotLinked:
		if err := mkdirs(e.DestAbs); err != nil {
			return err
		}
		if o.DryRun {
			report(o, action)
			return nil
		}
		return os.Symlink(e.SrcAbs, e.DestAbs)

	case StatusStale:
		if !o.Force {
			return fmt.Errorf("stale link %s -> %s (use --force)", e.DestAbs, readLink(e.DestAbs))
		}
		if err := os.Remove(e.DestAbs); err != nil {
			return err
		}
		if o.DryRun {
			report(o, action)
			return nil
		}
		return os.Symlink(e.SrcAbs, e.DestAbs)

	case StatusConflict:
		if !o.Backup {
			return fmt.Errorf("conflict at %s (real file; use --backup)", e.DestAbs)
		}
		bak := e.DestAbs + ".dotfiles.bak." + time.Now().Format("20060102-150405")
		if o.DryRun {
			report(o, "backup "+e.DestAbs+" -> "+bak)
			report(o, action)
			return nil
		}
		if err := os.Rename(e.DestAbs, bak); err != nil {
			return err
		}
		if err := mkdirs(e.DestAbs); err != nil {
			return err
		}
		return os.Symlink(e.SrcAbs, e.DestAbs)
	}
	return nil
}

// unlinkEntry 移除 Entry 指向仓库的符号链接。
func unlinkEntry(e Entry, o Options) error {
	if e.ResolveErr != nil {
		return e.ResolveErr // 显式指定时提示;全量模式在 applyLinks 中跳过
	}
	status := statusOf(e)
	if status != StatusLinked && status != StatusStale {
		return nil
	}
	if o.DryRun {
		report(o, "unlink "+e.DestAbs)
		return nil
	}
	if err := os.Remove(e.DestAbs); err != nil {
		return err
	}
	// 清理空的父目录链
	dir := filepath.Dir(e.DestAbs)
	for i := 0; i < 3; i++ {
		if isEmpty(dir) {
			_ = os.Remove(dir)
			dir = filepath.Dir(dir)
			continue
		}
		break
	}
	return nil
}

func pathsEqual(target, src string) bool {
	t, err := filepath.Abs(target)
	if err != nil {
		return target == src
	}
	return filepath.Clean(t) == filepath.Clean(src)
}

func readLink(p string) string {
	if t, err := os.Readlink(p); err == nil {
		return t
	}
	return "?"
}

// mkdirs 创建 dest 的父目录。
func mkdirs(dest string) error {
	return os.MkdirAll(filepath.Dir(dest), 0o755)
}

func isEmpty(dir string) bool {
	ents, err := os.ReadDir(dir)
	return err == nil && len(ents) == 0
}

func report(o Options, msg string) {
	if !o.Quiet {
		fmt.Println(msg)
	}
}

// Describe 返回 Entry 的可读描述行。
func (e Entry) Describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %-28s", statusTag(e.Status), e.Link.Src)
	if e.ResolveErr != nil {
		fmt.Fprintf(&b, " -> %s  (%s)", e.Link.Dest, e.ignoreReason())
		return b.String()
	}
	fmt.Fprintf(&b, " -> %s", e.DestAbs)
	if e.Message != "" {
		b.WriteString("  (" + e.Message + ")")
	}
	return b.String()
}

// ignoreReason 返回 ref-unset 条目的忽略原因展示文本(如 "未设置 {space_labeler}")。
func (e Entry) ignoreReason() string {
	if e.Message == "" {
		return e.ResolveErr.Error()
	}
	keys := strings.Split(e.Message, ", ")
	if len(keys) == 1 {
		return "未设置 {" + keys[0] + "}; dotf path set " + keys[0] + " <dir> 后生效"
	}
	return "未设置 {" + e.Message + "}"
}

// statusTag 返回状态列的彩色标签:linked 绿 / not-linked 青 / stale 黄 / ref-unset 灰(忽略) / missing-src、conflict 红。
func statusTag(s Status) string {
	switch s {
	case StatusLinked:
		return okTag("linked", 22)
	case StatusNotLinked:
		return step("not-linked", 22)
	case StatusStale:
		return outR.tag(ansiBold+ansiYellow, s.String(), 22)
	case StatusRefUnset:
		return dimTag("ref-unset", 22)
	default:
		return failTag(s.String(), 22)
	}
}
