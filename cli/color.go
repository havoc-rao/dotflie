// 彩色输出辅助:仅为终端(TTY)输出添加 ANSI 颜色。
// 管道/重定向、NO_COLOR 环境变量或 TERM=dumb 时自动退化为纯文本,
// 不污染脚本与 --json 输出;stdout 与 stderr 分别独立探测。
package cli

import (
	"fmt"
	"os"
)

// ANSI 样式码(仅前景色,不设背景,兼容浅/深色终端主题)。
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiBlue   = "\x1b[34m"
	ansiCyan   = "\x1b[36m"
	ansiGray   = "\x1b[90m"
)

// renderer 表示一个输出流的颜色开关(stdout/stderr 各自独立探测)。
type renderer struct{ on bool }

func newRenderer(f *os.File) renderer { return renderer{on: colorOn(f)} }

// colorOn 判断 f 是否为支持 ANSI 颜色的终端输出。
func colorOn(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

var (
	outR = newRenderer(os.Stdout) // 常规输出(stdout)
	errR = newRenderer(os.Stderr) // 错误/警告输出(stderr)
)

// paint 为 s 上色;输出流不支持颜色时原样返回。
func (r renderer) paint(code, s string) string {
	if code == "" || !r.on {
		return s
	}
	return code + s + ansiReset
}

// tag 返回定宽彩色标签:先按纯文本左对齐补齐到 w 个字符,再整体上色。
// ANSI 控制码不计入宽度,着色不会破坏列对齐;w<=0 表示不补齐。
func (r renderer) tag(code, s string, w int) string {
	if w > 0 {
		s = pad(s, w)
	}
	return r.paint(code, s)
}

// ---- stdout 样式 ----

// step 操作标签(青色粗体):linked/unlinked/removed/moved/recorded 等。
func step(s string, w int) string { return outR.tag(ansiBold+ansiCyan, s, w) }

// okTag 成功标签(绿色粗体):ok/clean/linked/committed 等。
func okTag(s string, w int) string { return outR.tag(ansiBold+ansiGreen, s, w) }

// failTag 失败标签(红色粗体):ERROR/FAILED 等。
func failTag(s string, w int) string { return outR.tag(ansiBold+ansiRed, s, w) }

// field 行首字段名(青色粗体):remote:/branch:/local:/root: 等。
func field(s string) string { return outR.paint(ansiBold+ansiCyan, s) }

// warn 警示文案(黄色粗体)。
func warn(s string) string { return outR.paint(ansiBold+ansiYellow, s) }

// info 强调文案(蓝色粗体):版本号、标题等。
func info(s string) string { return outR.paint(ansiBold+ansiBlue, s) }

// dim 次要文案(灰色):来源标记、非关键提示等。
func dim(s string) string { return outR.paint(ansiGray, s) }

// ---- stderr 样式 ----

// eErr 错误文案(红色)。
func eErr(s string) string { return errR.paint(ansiRed, s) }

// eFail 错误标签(红色粗体):dotf: 前缀、ERROR 列等。
func eFail(s string, w int) string { return errR.tag(ansiBold+ansiRed, s, w) }

// eWarn 警告文案(黄色粗体):dotf: warning: 前缀等。
func eWarn(s string) string { return errR.paint(ansiBold+ansiYellow, s) }

// projectStatus 项目状态的彩色标签:linked 绿 / not-linked 黄 / (path not set) 红。
func projectStatus(s string) string {
	switch s {
	case "linked":
		return okTag(s, 0)
	case "not-linked":
		return outR.tag(ansiBold+ansiYellow, s, 0)
	case "(path not set)":
		return failTag(s, 0)
	default:
		return s
	}
}

// PrintErr 以红色 "dotf: <err>" 形式打印错误到 stderr(main 使用)。
func PrintErr(err error) {
	fmt.Fprintf(os.Stderr, "%s %v\n", eFail("dotf:", 0), err)
}