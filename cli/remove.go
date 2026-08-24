// dotf remove 子命令:add 的逆操作——校验链接归属后,删链接、把文件移回原路径(恢复真实性)、
// 从清单删除条目。不做任何未校验的删除,绝不破坏目标位置的真实文件。
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Usage: dotf remove <目标...> [--dry-run] [--quiet]
func cmdRemove(args []string) error {
	dry := false
	quiet := false
	targets := parseFlags(args, map[string]*bool{"--dry-run": &dry, "--quiet": &quiet}, nil)
	if len(targets) == 0 {
		return fmt.Errorf("usage: dotf remove <目标...> (按 src/dest 名称匹配;add 的逆操作,文件移回原路径)")
	}
	m, root, err := load()
	if err != nil {
		return err
	}
	entries, err := Collect(m, root, targets)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no matching links")
	}
	failed := 0
	for _, e := range entries {
		if e.ResolveErr != nil {
			failed++
			if quiet {
				fmt.Fprintf(os.Stderr, "%s %v\n", eFail("dotf:", 0), e.ResolveErr)
			} else {
				fmt.Printf("%s %s\n", failTag("ERROR", 22), e.Link.Src)
				fmt.Fprintf(os.Stderr, "%s\n", eErr("  "+e.ResolveErr.Error()))
			}
			continue
		}
		if err := removeOne(m, root, e, quiet, dry); err != nil {
			failed++
			if quiet {
				fmt.Fprintf(os.Stderr, "%s %v\n", eFail("dotf:", 0), err)
			} else {
				fmt.Printf("%s %s\n", failTag("ERROR", 22), e.Link.Src)
				fmt.Fprintf(os.Stderr, "%s\n", eErr("  "+err.Error()))
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d failed", failed)
	}
	return nil
}

// removeOne 回滚单个条目:校验 → 删链接 → mv 回原路径 → 清单删条目 → 清理空目录。
func removeOne(m *Manifest, root string, e Entry, quiet, dry bool) error {
	// 1) 安全校验:dest 必须是指向本仓库 src 的符号链接
	fi, err := os.Lstat(e.DestAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("not linked (nothing at %s)", e.DestAbs)
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s is a real file, not a link; remove manually", e.DestAbs)
	}
	target, _ := os.Readlink(e.DestAbs)
	if !pathsEqual(target, e.SrcAbs) {
		return fmt.Errorf("%s points elsewhere (%s), not to repo; remove manually", e.DestAbs, target)
	}
	// 2) 源文件必须还在仓库,否则无法恢复真实性
	if _, err := os.Lstat(e.SrcAbs); err != nil {
		return fmt.Errorf("repo source missing: %s (cannot restore; fix manifest manually)", e.SrcAbs)
	}
	action := "remove " + e.DestAbs + " (restore from " + e.SrcAbs + ")"
	if dry {
		report(Options{Quiet: quiet}, action)
		return nil
	}
	// 3) 删链接 → mv 回原路径
	if err := os.Remove(e.DestAbs); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(e.DestAbs), 0o755); err != nil {
		return err
	}
	if err := os.Rename(e.SrcAbs, e.DestAbs); err != nil {
		// 尽力恢复链接,避免目标位置裸奔
		_ = os.Symlink(e.SrcAbs, e.DestAbs)
		return fmt.Errorf("move back failed: %w", err)
	}
	// 4) 清单删除该 src 条目(所有匹配)
	mpath, err := manifestPathOf(root)
	if err != nil {
		return err
	}
	if err := dropLink(mpath, e.Link.Src); err != nil {
		return err
	}
	// 5) 清理仓库侧空父目录链(不越过仓库根)
	cleanupEmptyDirs(filepath.Dir(e.SrcAbs), root)
	if !quiet {
		fmt.Printf("%s %s -> %s (restored)\n", step("removed", 22), e.Link.Src, e.DestAbs)
	}
	return nil
}

// manifestPathOf 定位仓库清单文件路径(优先 .dotfiles.yaml)。
func manifestPathOf(root string) (string, error) {
	for _, name := range DefaultNames {
		p := filepath.Join(root, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("manifest not found under %s", root)
}

// dropLink 从清单删除所有 src 匹配的条目并写回。
func dropLink(mpath, src string) error {
	data, err := os.ReadFile(mpath)
	if err != nil {
		return err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parse %s: %w", mpath, err)
	}
	var kept []LinkSpec
	for _, l := range m.Links {
		if l.Src != src {
			kept = append(kept, l)
		}
	}
	m.Links = kept
	return saveManifest(mpath, &m)
}

// cleanupEmptyDirs 从 dir 向上删除空目录,直到仓库根(不含根)。
func cleanupEmptyDirs(dir, root string) {
	for dir != root && filepath.Dir(dir) != dir {
		if isEmpty(dir) {
			_ = os.Remove(dir)
		} else {
			break
		}
		dir = filepath.Dir(dir)
	}
}
