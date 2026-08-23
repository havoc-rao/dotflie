// dotf project 子命令:项目级规则一键收编(自动设 key/探测规则目录/建 .shr-dir 标记/收编)。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Usage: dotf project add <key> <项目根> [--dir <规则子目录>]
//
//	dotf project list
func cmdProject(args []string) error {
	if len(args) == 0 {
		usageProject()
		return nil
	}
	switch args[0] {
	case "add":
		return cmdProjectAdd(args[1:])
	case "list":
		return cmdProjectList(args[1:])
	default:
		return fmt.Errorf("unknown project subcommand %q (add|list)", args[0])
	}
}

// cmdProjectAdd <key> <项目根> --store <tool> [--dir <规则子目录>]:
// key 与存储归属(--store)由用户显式设定,无默认;
// 存储位置为 <仓库>/<store>/projects/<key>/,path 变量衍生为 key → 项目根。
// dotf 只做通用收编:规则目录本体(及其中任意文件)收进仓库并链接回原处;
// 工具如何发现规则位置(注册表/约定目录/自己的标记)由工具自理,dotf 不介入。
func cmdProjectAdd(args []string) error {
	dir := ""
	store := ""
	rest := parseFlags(args, nil, map[string]*string{"--dir": &dir, "--store": &store})
	if len(rest) != 2 {
		return fmt.Errorf("usage: dotf project add <key> <项目根> --store <tool> [--dir <规则子目录>]")
	}
	key, projArg := rest[0], rest[1]
	if !ValidPathKey(key) {
		return fmt.Errorf("invalid key %q (letters/digits/underscore, start with letter or _)", key)
	}
	if store == "" {
		return fmt.Errorf("--store is required (archive owner, e.g. --store shr; dotf 泛用,不内置任何工具的存储假设)")
	}
	if !validStoreName(store) {
		return fmt.Errorf("invalid --store %q (letters/digits/underscore/hyphen)", store)
	}
	proj, err := filepath.Abs(projArg)
	if err != nil {
		return err
	}
	proj = filepath.Clean(proj)
	if fi, err := os.Stat(proj); err != nil || !fi.IsDir() {
		return fmt.Errorf("project not found: %s", proj)
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	_, root, err := Find(wd)
	if err != nil {
		return fmt.Errorf("no manifest found (run dotf init first)")
	}
	// 规则子目录:--dir 指定,或自动探测 .shr / .vscode/shr
	if dir == "" {
		dir = detectRulesDir(proj)
		if dir == "" {
			return fmt.Errorf("no rules dir found under %s (expected .shr or .vscode/shr; use --dir)", proj)
		}
	}
	rd := filepath.Clean(filepath.FromSlash(dir))
	if filepath.IsAbs(rd) || strings.HasPrefix(rd, "..") {
		return fmt.Errorf("invalid --dir %q (must be relative to project root)", dir)
	}
	rulesPath := filepath.Join(proj, rd)
	if fi, err := os.Stat(rulesPath); err != nil || !fi.IsDir() {
		return fmt.Errorf("rules dir not found: %s", rulesPath)
	}
	// 记录本机路径(key → 项目根,私有 .dotfiles.env)
	if err := setEnvPath(root, key, proj); err != nil {
		return err
	}
	fmt.Printf("%s  %s/projects/%s/ %s\n", field("store:"), info(store), info(key), dim("(archive location)"))
	// 通用收编:规则目录本体(工具发现机制由工具自理)
	if err := addOne(rulesPath, store+"/projects/"+key+"/"+filepath.ToSlash(rd), false, false); err != nil {
		fmt.Printf("%s: %v\n", warn("skip rules dir"), err)
	}
	return nil
}

// validStoreName 校验存储归属名:字母/数字/下划线/连字符。
func validStoreName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
		if !ok {
			return false
		}
	}
	return true
}

// setEnvPath 把 key → dir 写入私有 .dotfiles.env(已存在同值则跳过)。
func setEnvPath(root, key, dir string) error {
	paths, err := LoadEnvPaths(root)
	if err != nil {
		return err
	}
	if paths[key] == dir {
		return nil
	}
	paths[key] = dir
	if err := SaveEnvPaths(root, paths); err != nil {
		return err
	}
	fmt.Printf("%s %s = %s %s\n", step("path", 0), info(key), dir, dim("(private .dotfiles.env)"))
	return nil
}

// detectRulesDir 探测项目规则子目录:.shr 优先,其次 .vscode/shr。
func detectRulesDir(proj string) string {
	for _, c := range []string{".shr", ".vscode/shr"} {
		if fi, err := os.Stat(filepath.Join(proj, c)); err == nil && fi.IsDir() {
			return c
		}
	}
	return ""
}

func cmdProjectList(args []string) error {
	m, root, err := load()
	if err != nil {
		return err
	}
	type row struct{ store, key, path, status string }
	var rows []row
	merged := mergedPathsOf(m, root)
	matches, err := filepath.Glob(filepath.Join(root, "*", "projects"))
	if err == nil {
		for _, dir := range matches {
			store := filepath.Base(filepath.Dir(dir))
			subs, _ := os.ReadDir(dir)
			for _, d := range subs {
				if !d.IsDir() {
					continue
				}
				key := d.Name()
				p := merged[key]
				status := "-"
				if p == "" {
					status = "(path not set)"
				} else {
					// 从清单找该 key 的规则目录条目(最短 src,排除 .shr-dir 标记),
					// 展开其 dest 判断是否已链接——不依赖 shr 专属标记,通用
					bestSrc, bestDest := "", ""
					for _, l := range m.Links {
						if strings.HasPrefix(l.Src, store+"/projects/"+key+"/") &&
							!strings.HasSuffix(l.Src, "/.shr-dir") {
							if bestSrc == "" || len(l.Src) < len(bestSrc) {
								bestSrc, bestDest = l.Src, l.Dest
							}
						}
					}
					if bestDest != "" {
						if abs, err := (&Manifest{Links: m.Links, Paths: merged}).DestAbs(bestDest); err == nil {
							if fi, err := os.Lstat(abs); err == nil && fi.Mode()&os.ModeSymlink != 0 {
								status = "linked"
							} else {
								status = "not-linked"
							}
						}
					}
				}
				rows = append(rows, row{store, key, p, status})
			}
		}
	}
	if len(rows) == 0 {
		fmt.Println("(无已收编 project;使用: dotf project add <key> <项目根>)")
		return nil
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].store != rows[j].store {
			return rows[i].store < rows[j].store
		}
		return rows[i].key < rows[j].key
	})
	fmt.Printf("%s %s %s %s\n",
		outR.tag(ansiBold, "store", 10), outR.tag(ansiBold, "key", 22), outR.tag(ansiBold, "project path", 46), outR.tag(ansiBold, "status", 0))
	for _, r := range rows {
		fmt.Printf("%s %s %s %s\n",
			outR.tag(ansiBold+ansiCyan, r.store, 10),
			outR.tag(ansiBold+ansiCyan, r.key, 22),
			r.path,
			projectStatus(r.status))
	}
	return nil
}

// mergedPathsOf 返回合并后的 paths(manifest + host + env)。
func mergedPathsOf(m *Manifest, root string) map[string]string {
	over, _ := LoadHostPaths(root)
	env, _ := LoadEnvPaths(root)
	return mergePaths(mergePaths(m.Paths, over), env)
}

func usageProject() {
	fmt.Print(`dotf project - 项目级配置一键收编

项目级配置属于项目本身,路径各机不同:dotf 把内容收进仓库
<仓库>/<store>/projects/<key>/,链接回原处,实现跨机同步。
--store 指定归档归属(无默认,dotf 泛用)。工具如何发现规则位置
(注册表/约定目录/自己的标记)由工具自理,dotf 不介入。
key 与 --store 由用户显式设定,不做推断。

子命令:
  dotf project add <key> <项目根> --store <tool> [--dir <规则子目录>]
                        一键完成:写 path(key→项目根,私有 .dotfiles.env)、
                        探测规则目录(.shr/.vscode/shr)、收编到
                        <store>/projects/<key>/、建立链接
  dotf project list     列出已收编项目及其链接状态

示例:
  dotf project add shr ~/Projects/tools/shr --store shr
  dotf project add space_labeler ~/Projects/macOS/space-labeler --store shr
  dotf project add xy ~/Projects/xy --store other --dir .config/rules
  dotf project list
`)
}
