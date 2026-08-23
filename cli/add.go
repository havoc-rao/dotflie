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
	if srcAs == "" {
		srcAs = defaultSrc(dest)
	}
	return addOne(dest, srcAs, force, noLink)
}

// addOne 收编单个路径:mv 进仓库、追加清单、建立链接。
// 供 dotf add 与 dotf project add 复用;src 已存在且非 force 时返回已存在错误。
func addOne(dest, srcAs string, force, noLink bool) error {
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
	src := filepath.Clean(filepath.Join(root, srcAs))
	exists := false
	if _, err := os.Lstat(src); err == nil {
		exists = true
		if !force {
			return fmt.Errorf("%s already exists in repo (use --force)", srcAs)
		}
	}
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		return err
	}
	if force && exists {
		_ = os.RemoveAll(src)
	}
	if err := os.Rename(dest, src); err != nil {
		return fmt.Errorf("move %s -> %s: %w (same filesystem required)", dest, src, err)
	}
	fmt.Printf("%s %s -> %s\n", step("moved", 22), dest, src)
	// 追加清单条目(dest 命中已设 paths 前缀时用 {key} 占位记录)
	link := LinkSpec{Src: srcAs, Dest: inferPathRef(dest, pathsOf(root))}
	if err := appendLink(mpath, link); err != nil {
		return err
	}
	fmt.Printf("%s %s -> %s\n", step("recorded", 22), srcAs, link.Dest)
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
		fmt.Printf("%s %s\n", okTag("linked", 22), dest)
	} else {
		fmt.Printf("%s\n", warn("skipped linking (--no-link): run `dotf link "+srcAs+"` to link"))
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
	return saveManifest(mpath, &m)
}

// saveManifest 写回清单(头部注释 + links + paths;自动排序输出由 yaml.v3 处理)。
func saveManifest(mpath string, m *Manifest) error {
	var out any = map[string]any{"links": m.Links}
	if len(m.Paths) > 0 {
		out = map[string]any{"links": m.Links, "paths": m.Paths}
	}
	outData, err := yaml.Marshal(out)
	if err != nil {
		return err
	}
	hdr := "# dotfiles 映射清单(src 相对仓库根;dest 支持 ~、$ENV 与 {paths} 引用;由 dotf add/link/remove 维护)\n"
	return os.WriteFile(mpath, append([]byte(hdr), outData...), 0o644)
}
