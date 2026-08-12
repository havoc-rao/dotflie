// 清单解析与查找（原 internal/manifest，整合进 cli 包）。
package cli

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest 是 .dotfiles.yaml 的结构。
type Manifest struct {
	// Links 是 src -> dest 的映射列表。
	Links []LinkSpec `yaml:"links"`
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
	if len(m.Links) == 0 {
		return nil, errors.New("manifest has no links")
	}
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

// DestAbs 展开 ~ 与环境变量并返回绝对目标路径。
func (m *Manifest) DestAbs(dest string) (string, error) {
	expanded := os.ExpandEnv(dest)
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

// DefaultTemplate 是 init 命令写入的模板。
const DefaultTemplate = `# dotfiles 映射清单
# src 相对本仓库根目录; dest 为目标绝对路径(支持 ~ 与 $ENV)。
# 目录条目整体符号链接(不展开内容),便于整目录同步。
links:
  - src: zsh/.zshrc
    dest: ~/.zshrc
  - src: git/.gitconfig
    dest: ~/.gitconfig
  - src: nvim
    dest: ~/.config/nvim
`
