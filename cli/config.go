// dotf config 子命令 + 工具级配置(仓库根目录等)。
// 配置文件: $XDG_CONFIG_HOME/dotf/config.toml 或 ~/.config/dotf/config.toml。
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config 是工具级配置(目前仅仓库根)。
type Config struct {
	Root string `toml:"root"`
}

func configPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "dotf", "config.toml")
}

// LoadConfig 读取工具配置;不存在时返回空配置。
func LoadConfig() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	c := &Config{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) == 2 && strings.TrimSpace(kv[0]) == "root" {
			c.Root = strings.Trim(strings.TrimSpace(kv[1]), `"`)
			if c.Root == "" {
				return nil, fmt.Errorf("parse %s: invalid root", configPath())
			}
			return c, nil
		}
	}
	return c, nil
}

// SaveRoot 记录仓库根(init 自动调用/用户手动 set-root)。
func SaveRoot(root string) error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(fmt.Sprintf("root = %q\n", root)), 0o644)
}

// ClearRoot 清除仓库根配置。
func ClearRoot() error {
	return os.Remove(configPath())
}

// ConfiguredRoot 返回配置的仓库根;未配置返回空串。
func ConfiguredRoot() string {
	c, err := LoadConfig()
	if err != nil || c.Root == "" {
		return ""
	}
	return c.Root
}

// ---- dotf config 子命令 ----

func cmdConfig(args []string) error {
	if len(args) == 0 {
		if r := ConfiguredRoot(); r != "" {
			fmt.Printf("root: %s\n", r)
			fmt.Printf("config: %s\n", configPath())
		} else {
			fmt.Println("root: (未设置 — 使用当前目录向上查找清单;可用 dotf config set-root <dir>)")
		}
		return nil
	}
	switch args[0] {
	case "set-root":
		if len(args) != 2 {
			return fmt.Errorf("usage: dotf config set-root <dir>")
		}
		root, err := filepath.Abs(args[1])
		if err != nil {
			return err
		}
		root = filepath.Clean(root)
		if err := SaveRoot(root); err != nil {
			return err
		}
		fmt.Printf("root 已设定: %s\n", root)
		return nil
	case "unset-root":
		if err := ClearRoot(); err != nil {
			if os.IsNotExist(err) {
				fmt.Println("root: (未设置)")
				return nil
			}
			return err
		}
		fmt.Println("root 已清除,恢复从当前目录向上查找")
		return nil
	default:
		return fmt.Errorf("unknown config subcommand %q (set-root|unset-root)", args[0])
	}
}
