// dotf path 子命令:管理机器无关路径变量(写 .dotfiles.<hostname>.yaml)。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func cmdPath(args []string) error {
	if len(args) == 0 {
		usagePath()
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return cmdPathList(rest)
	case "get":
		return cmdPathGet(rest)
	case "set":
		return cmdPathSet(rest)
	case "unset":
		return cmdPathUnset(rest)
	default:
		return fmt.Errorf("unknown path subcommand %q (try: dotf path help)", sub)
	}
}

// findRoot 定位仓库根(清单所在目录);未找到时给出提示。
func findRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	_, root, err := Find(wd)
	if err != nil {
		return "", fmt.Errorf("no manifest found (run dotf init first)")
	}
	return root, nil
}

// pathSourceOf 返回 key 的来源:私有 .env > 共享 hostname 文件 > 主清单默认。
func pathSourceOf(root, key string, over, env map[string]string) string {
	if _, ok := env[key]; ok {
		return "env"
	}
	if _, ok := over[key]; ok {
		return "host"
	}
	return "manifest"
}

func cmdPathList(args []string) error {
	asJSON := false
	parseFlags(args, map[string]*bool{"--json": &asJSON}, nil)
	root, err := findRoot()
	if err != nil {
		return err
	}
	m, _, err := load()
	if err != nil {
		return err
	}
	over, err := LoadHostPaths(root)
	if err != nil {
		return err
	}
	env, err := LoadEnvPaths(root)
	if err != nil {
		return err
	}
	// 合并后全集:env > host > manifest
	merged := mergePaths(mergePaths(m.Paths, over), env)
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if asJSON {
		out := make([]map[string]string, 0, len(keys))
		for _, k := range keys {
			out = append(out, map[string]string{
				"key":    k,
				"value":  merged[k],
				"source": pathSourceOf(root, k, over, env),
			})
		}
		return printJSON(out)
	}
	if len(keys) == 0 {
		fmt.Println("(no paths set; use: dotf path set <key> <dir>)")
	}
	for _, k := range keys {
		mark := " "
		if fi, err := os.Stat(merged[k]); err != nil || !fi.IsDir() {
			mark = "?"
		}
		fmt.Printf("%s %-24s %s  [%s]\n", mark, k, merged[k], pathSourceOf(root, k, over, env))
	}
	// 提示 links 中引用了但未定义的 key
	for _, k := range m.MissingRefs() {
		fmt.Printf("! %s referenced in links but not set (use: dotf path set %s <dir>)\n", k, k)
	}
	return nil
}

func cmdPathGet(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: dotf path get <key>")
	}
	key := args[0]
	_, _, err := load()
	if err != nil {
		return err
	}
	return printPathKey(key)
}

func printPathKey(key string) error {
	if !ValidPathKey(key) {
		return fmt.Errorf("invalid key %q (letters/digits/underscore, start with letter or _)", key)
	}
	m, _, err := load()
	if err != nil {
		return err
	}
	v, ok := m.Paths[key]
	if !ok {
		return fmt.Errorf("path ref not set: {%s} (use: dotf path set %s <dir>)", key, key)
	}
	fmt.Println(v)
	return nil
}

// expandDir 展开 ~ 与 $ENV 后返回绝对路径。
func expandDir(dir string) (string, error) {
	expanded := os.ExpandEnv(dir)
	if strings.HasPrefix(expanded, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if expanded == "~" {
			expanded = home
		} else if strings.HasPrefix(expanded, "~/") {
			expanded = filepath.Join(home, expanded[2:])
		}
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// pathStore 描述 paths 的写入目标:默认私有 .env;--shared 写共享 hostname 文件。
type pathStore struct {
	load func(root string) (map[string]string, error)
	save func(root string, paths map[string]string) error
	path func(root string) string
	kind string
}

func envStore() pathStore {
	return pathStore{LoadEnvPaths, SaveEnvPaths, EnvPath, "env(private,gitignored)"}
}

func hostStore() pathStore {
	return pathStore{LoadHostPaths, SaveHostPaths, HostPath, "host(shared)"}
}

func pickStore(args []string) (pathStore, []string, error) {
	shared := false
	rest := parseFlags(args, map[string]*bool{"--shared": &shared}, nil)
	if shared {
		return hostStore(), rest, nil
	}
	return envStore(), rest, nil
}

func cmdPathSet(args []string) error {
	store, rest, err := pickStore(args)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return fmt.Errorf("usage: dotf path set [--shared] <key> <dir>")
	}
	key, dir := rest[0], rest[1]
	if !ValidPathKey(key) {
		return fmt.Errorf("invalid key %q (letters/digits/underscore, start with letter or _)", key)
	}
	abs, err := expandDir(dir)
	if err != nil {
		return err
	}
	root, err := findRoot()
	if err != nil {
		return err
	}
	paths, err := store.load(root)
	if err != nil {
		return err
	}
	if paths[key] == abs {
		fmt.Printf("%s = %s (unchanged)\n", key, abs)
		return nil
	}
	paths[key] = abs
	if err := store.save(root, paths); err != nil {
		return err
	}
	fmt.Printf("%s = %s  ->  %s  [%s]\n", key, abs, store.path(root), store.kind)
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "dotf: warning: %s does not exist yet\n", abs)
	}
	return nil
}

func cmdPathUnset(args []string) error {
	store, rest, err := pickStore(args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: dotf path unset [--shared] <key>")
	}
	key := rest[0]
	root, err := findRoot()
	if err != nil {
		return err
	}
	paths, err := store.load(root)
	if err != nil {
		return err
	}
	if _, ok := paths[key]; !ok {
		return fmt.Errorf("not set: %s", key)
	}
	delete(paths, key)
	if err := store.save(root, paths); err != nil {
		return err
	}
	fmt.Printf("unset %s (from %s)\n", key, store.path(root))
	return nil
}

func usagePath() {
	fmt.Print(`dotf path - 管理机器路径变量(paths)

paths 是清单里 dest 的 {key} 占位符,三层来源(优先级由低到高):
  1. .dotfiles.yaml 的 paths 段          共享默认值(随仓库提交)
  2. .dotfiles.<hostname>.yaml          本机共享覆盖(dotf path set --shared)
  3. .dotfiles.env                      本机私有覆盖(默认,已 gitignore 不提交)

子命令:
  dotf path list [--json]       列出全部 paths(含来源 env/host/manifest)
  dotf path get <key>           查看单个 key 的值
  dotf path set [--shared] <key> <dir>
                                设定映射(默认写私有 .dotfiles.env;--shared 写共享 hostname 文件)
  dotf path unset [--shared] <key>
                                删除映射(默认删私有)

示例:
  dotf path set space_labeler ~/Documents/Projects/tools/macOS/space-labeler
  dotf path list
`)
}
