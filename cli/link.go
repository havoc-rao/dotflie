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
}

// Options 控制链接行为。
type Options struct {
	DryRun bool // 只打印将要执行的操作
	Force  bool // 替换指向别处的符号链接
	Backup bool // 冲突时把现有文件备份为 .dotfiles.bak.<ts>
	Quiet  bool
}

// Collect 解析 manifest 并组装 Entry 列表。targets 为空表示全部。
func Collect(m *Manifest, repoRoot string, targets []string) ([]Entry, error) {
	var out []Entry
	for _, l := range m.Links {
		dest, err := m.DestAbs(l.Dest)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", l.Dest, err)
		}
		e := Entry{Link: l, SrcAbs: m.SrcAbs(repoRoot, l.Src), DestAbs: dest}
		if len(targets) > 0 {
			name := filepath.Base(l.Src)
			matched := false
			for _, t := range targets {
				if t == name || t == l.Src || t == l.Dest || t == dest {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		e.Status = statusOf(e)
		out = append(out, e)
	}
	return out, nil
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
	fmt.Fprintf(&b, "%s %-28s -> %s", statusTag(e.Status), e.Link.Src, e.DestAbs)
	if e.Message != "" {
		b.WriteString("  (" + e.Message + ")")
	}
	return b.String()
}

// statusTag 返回状态列的彩色标签:linked 绿 / not-linked 青 / stale 黄 / missing-src、conflict 红。
func statusTag(s Status) string {
	switch s {
	case StatusLinked:
		return okTag("linked", 22)
	case StatusNotLinked:
		return step("not-linked", 22)
	case StatusStale:
		return outR.tag(ansiBold+ansiYellow, s.String(), 22)
	default:
		return failTag(s.String(), 22)
	}
}
