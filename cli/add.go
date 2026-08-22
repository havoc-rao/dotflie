// dotf add 子命令:把本机真实路径"收编"进仓库——自动 mkdir/mv、追加清单、建立链接。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Usage: dotf add <dest> [--as <src>] [--force] [--no-link]
func cmdAdd(args []string) error {
	srcAs := ""
	force := false
	noLink := false
	rest := parseFlags(args, map[string]*bool{
		"--force":   &force,
		"--no-link": &noLink,
	}, map[string]*string{"--as": &srcAs})
	if len(rest) != 1 {
		return fmt.Errorf("usage: dotf add <dest> [--as <src>] [--force] [--no-link]")
	}
	dest, err := expandDir(rest[0])
	if err != nil {
		return err
	}
	if _, err := os.Lstat(dest); err != nil {
		return fmt.Errorf("source not found: %s", dest)
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	mpath, root, err := Find(wd)
	if err != nil {
		return fmt.Errorf("no manifest found (run dotf init first)")
	}
	// 1) 决定归档 src:--as 指定,或自动镜像(dest 去掉 home 前缀)
	if srcAs == "" {
		srcAs = defaultSrc(dest)
	}
	src := filepath.Clean(filepath.Join(root, srcAs))
	if _, err := os.Lstat(src); err == nil && !force {
		return fmt.Errorf("%s already exists in repo (use --force)", srcAs)
	}
	// 2) mv 进仓库
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		return err
	}
	if force {
		_ = os.RemoveAll(src)
	}
	if err := os.Rename(dest, src); err != nil {
		return fmt.Errorf("move %s -> %s: %w (same filesystem required)", dest, src, err)
	}
	fmt.Printf("moved %s -> %s\n", dest, src)
	// 3) 追加清单条目(dest 命中已设 paths 前缀时用 {key} 占位记录)
	link := LinkSpec{Src: srcAs, Dest: inferPathRef(dest, pathsOf(root))}
	if err := appendLink(mpath, link); err != nil {
		return err
	}
	fmt.Printf("recorded: %s -> %s\n", srcAs, link.Dest)
	// 4) 立即建立链接
	if !noLink {
		m, _, err := load()
		if err != nil {
			return err
		}
		entries, err := Collect(m, root, []string{srcAs})
		if err != nil {
			return err
		}
		if len(entries) != 1 {
			return fmt.Errorf("unexpected entries for %s", srcAs)
		}
		if err := linkEntry(entries[0], Options{}); err != nil {
			return fmt.Errorf("link failed: %w", err)
		}
		fmt.Printf("linked %s\n", dest)
	} else {
		fmt.Printf("skipped linking (--no-link): run `dotf link %s` to link\n", srcAs)
	}
	return nil
}

// pathsOf 加载合并后的 paths(主清单 + host + env)。
func pathsOf(root string) map[string]string {
	over, _ := LoadHostPaths(root)
	env, _ := LoadEnvPaths(root)
	m, _, err := load()
	if err != nil {
		return map[string]string{}
	}
	return mergePaths(mergePaths(m.Paths, over), env)
}

// defaultSrc 自动镜像:dest 去掉 home 前缀后的相对路径。
func defaultSrc(dest string) string {
	home, _ := os.UserHomeDir()
	p := dest
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		p = strings.TrimPrefix(p, home+string(filepath.Separator))
	}
	return strings.TrimLeft(p, string(filepath.Separator))
}

// inferPathRef 在 paths 中找 dest 的最长路径前缀命中,写为 {key}/<后缀>;
// 未命中(如固定 home 路径)返回原样。
func inferPathRef(dest string, paths map[string]string) string {
	best, bestKey := "", ""
	for k, v := range paths {
		if dest == v || strings.HasPrefix(dest, v+string(filepath.Separator)) {
			if len(v) > len(best) {
				best, bestKey = v, k
			}
		}
	}
	if bestKey != "" {
		return "{" + bestKey + "}" + dest[len(best):]
	}
	// 未命中 path 前缀:dest 在 home 下则记录 ~/ 形式(可移植,换机自动展开)
	if home, err := os.UserHomeDir(); err == nil {
		sep := string(filepath.Separator)
		if strings.HasPrefix(dest, home+sep) {
			return "~" + dest[len(home):]
		}
	}
	return dest
}

// appendLink 宽松读取清单(允许空 links)、追加条目并写回(保留 paths 段)。
func appendLink(mpath string, link LinkSpec) error {
	data, err := os.ReadFile(mpath)
	if err != nil {
		return err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parse %s: %w", mpath, err)
	}
	m.Links = append(m.Links, link)
	var out any = map[string]any{"links": m.Links}
	if len(m.Paths) > 0 {
		out = map[string]any{"links": m.Links, "paths": m.Paths}
	}
	outData, err := yaml.Marshal(out)
	if err != nil {
		return err
	}
	hdr := "# dotfiles 映射清单(src 相对仓库根;dest 支持 ~、$ENV 与 {paths} 引用;由 dotf add/link 维护)\n"
	return os.WriteFile(mpath, append([]byte(hdr), outData...), 0o644)
}
