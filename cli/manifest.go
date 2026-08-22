// 清单解析与查找（原 internal/manifest，整合进 cli 包）。
package cli

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// envFile 是本机私有路径文件(KEY=value),默认 gitignore 不提交。
const envFile = ".dotfiles.env"

// Manifest 是 .dotfiles.yaml 的结构。
type Manifest struct {
	// Links 是 src -> dest 的映射列表。
	Links []LinkSpec `yaml:"links"`
	// Paths 是机器无关的路径变量;dest 中可用 {key} 引用。
	// 默认值写主清单,每机覆盖写 .dotfiles.<hostname>.yaml。
	Paths map[string]string `yaml:"paths"`
}

// LinkSpec 定义单个链接项。
type LinkSpec struct {
	// Src 相对仓库根目录（可以是指针文件或目录）。
	Src string `yaml:"src"`
	// Dest 目标绝对路径（支持 ~ 与 $VAR 展开）。
	Dest string `yaml:"dest"`
}

// DefaultNames 是候选 manifest 文件名（按优先级）。
var DefaultNames = []string{
	".dotfiles.yaml",
	".dotfiles.yml",
	"dotfiles.yaml",
	"dotfiles.yml",
}

// Load 从给定路径读取 manifest。
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := &Manifest{}
	if err := yaml.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	// 允许空清单(init 模板无示例条目):list/status 输出为空,link 报 no matching links。
	return m, nil
}

// Find 从 start 目录向上查找 manifest 文件，返回 (manifest路径, 仓库根目录)。
func Find(start string) (string, string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", "", err
	}
	for {
		for _, name := range DefaultNames {
			p := filepath.Join(dir, name)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p, dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", fmt.Errorf("manifest not found (looked for %s)", strings.Join(DefaultNames, ", "))
		}
		dir = parent
	}
}

// SrcAbs 返回 src 相对仓库根的绝对路径。
func (m *Manifest) SrcAbs(repoRoot, src string) string {
	if filepath.IsAbs(src) {
		return filepath.Clean(src)
	}
	return filepath.Join(repoRoot, filepath.Clean(src))
}

// DestAbs 展开 {paths.key}、~ 与环境变量并返回绝对目标路径。
// 引用了未定义的 {key} 时返回错误(提示用 dotf path set 设置),避免静默展开成空路径。
func (m *Manifest) DestAbs(dest string) (string, error) {
	expanded, err := expandRefs(dest, m.Paths)
	if err != nil {
		return "", err
	}
	expanded = os.ExpandEnv(expanded)
	if strings.HasPrefix(expanded, "~") {
		usr, err := user.Current()
		if err != nil {
			return "", fmt.Errorf("resolve home: %w", err)
		}
		if expanded == "~" {
			expanded = usr.HomeDir
		} else if strings.HasPrefix(expanded, "~/") {
			expanded = filepath.Join(usr.HomeDir, expanded[2:])
		}
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// ---- paths: 机器无关路径变量 + 每机覆盖 ----

var (
	// refPattern 匹配 dest 中的 {key} 引用。
	refPattern = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)
	// keyPattern 校验 paths 的 key 命名:字母/下划线开头,仅字母数字下划线。
	keyPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// ValidPathKey 校验 paths 的 key 命名:字母/下划线开头,仅字母数字下划线。
func ValidPathKey(key string) bool {
	return keyPattern.MatchString(key)
}

// expandRefs 把 dest 中的 {key} 替换为 paths[key];未定义则报错。
func expandRefs(s string, paths map[string]string) (string, error) {
	var missing []string
	out := refPattern.ReplaceAllStringFunc(s, func(tok string) string {
		if v, ok := paths[tok[1:len(tok)-1]]; ok {
			return v
		}
		missing = append(missing, tok[1:len(tok)-1])
		return tok
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("path ref not set: {%s} (use: dotf path set %s <dir>)",
			strings.Join(missing, ", "), missing[0])
	}
	return out, nil
}

// HostTag 返回本机标识:hostname 去掉 macOS 常见的 .local 后缀。
func HostTag() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		h = "unknown"
	}
	return strings.TrimSuffix(h, ".local")
}

// HostPath 返回本机覆盖文件路径:<仓库根>/.dotfiles.<hostname>.yaml。
func HostPath(root string) string {
	return filepath.Join(root, ".dotfiles."+HostTag()+".yaml")
}

// LoadHostPaths 读取本机覆盖文件的 paths(不存在时返回空 map)。
func LoadHostPaths(root string) (map[string]string, error) {
	p := HostPath(root)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var h struct {
		Paths map[string]string `yaml:"paths"`
	}
	if err := yaml.Unmarshal(data, &h); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	if h.Paths == nil {
		h.Paths = map[string]string{}
	}
	return h.Paths, nil
}

// SaveHostPaths 把 paths 写回本机覆盖文件(由 dotf path set/unset 维护)。
func SaveHostPaths(root string, paths map[string]string) error {
	p := HostPath(root)
	data, err := yaml.Marshal(map[string]any{"paths": paths})
	if err != nil {
		return err
	}
	hdr := "# dotf 本机路径映射:由 `dotf path set/unset` 维护(唯一机器相关文件,建议提交仓库)。\n"
	return os.WriteFile(p, []byte(hdr+string(data)), 0o644)
}

// mergePaths 合并两个映射,b 覆盖 a 的同名 key。
func mergePaths(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// EnvPath 返回本机私有路径文件:<仓库根>/.dotfiles.env(默认 gitignore,不提交)。
func EnvPath(root string) string {
	return filepath.Join(root, envFile)
}

// LoadEnvPaths 读取 .dotfiles.env 的 paths(不存在时返回空 map)。
func LoadEnvPaths(root string) (map[string]string, error) {
	p := EnvPath(root)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	return parseEnv(p, data)
}

// parseEnv 解析 KEY=value 行(# 注释与空行忽略;值保留原样,由 dest 展开阶段统一处理 ~ 与 $ENV)。
func parseEnv(p string, data []byte) (map[string]string, error) {
	out := map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq <= 0 {
			return nil, fmt.Errorf("parse %s: line %d: expected KEY=value", p, i+1)
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if !ValidPathKey(key) {
			return nil, fmt.Errorf("parse %s: line %d: invalid key %q", p, i+1, key)
		}
		out[key] = val
	}
	return out, nil
}

// SaveEnvPaths 以 KEY=value 格式把 paths 写回 .dotfiles.env(自动排序)。
func SaveEnvPaths(root string, paths map[string]string) error {
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# dotf 本机路径(私有,已 gitignore;由 `dotf path set/unset` 维护)。\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, paths[k])
	}
	return os.WriteFile(EnvPath(root), []byte(b.String()), 0o644)
}

// MissingRefs 扫描 links 中引用的 {key},返回未在 paths 中定义的 key 列表。
func (m *Manifest) MissingRefs() []string {
	var missing []string
	seen := map[string]bool{}
	for _, l := range m.Links {
		for _, match := range refPattern.FindAllStringSubmatch(l.Dest, -1) {
			key := match[1]
			if _, ok := m.Paths[key]; !ok && !seen[key] {
				seen[key] = true
				missing = append(missing, key)
			}
		}
	}
	return missing
}

// DefaultTemplate 是 init 命令写入的模板(不含示例条目,避免 status 出现 missing-src 噪音)。
const DefaultTemplate = `# dotfiles 映射清单
# src 相对本仓库根目录; dest 为目标绝对路径(支持 ~、$ENV 与 {paths} 引用)。
# 目录条目整体符号链接(不展开内容),便于整目录同步。
#
# 收编本机路径:  dotf add <dest> [--as <src>]    (自动 mv 进仓库、记录清单并建链)
# 本机路径变量:  dotf path set <key> <dir>       (默认写私有 .dotfiles.env,已 gitignore)
# 项目类路径示例(dest 引用 {key} 建议加引号):
#   dotf path set space_labeler ~/Projects/macOS/space-labeler
#   dotf add ~/Projects/macOS/space-labeler/.vscode/shr --as shr/projects/space_labeler/.vscode/shr
links: []
`
