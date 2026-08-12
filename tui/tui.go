// Package tui: dotfiles 的过滤选择器 TUI 基础组件。
//
// 设计风格参考 shr/tui:带实时输入过滤的候选选择器(bubbletea),
// 布局自上而下为 标题栏 / ── 分隔线 ── / 列表(光标行绿色粗体 + 命中高亮) /
// 输入行(❯ 闪烁光标) / 状态栏(计数 · 快捷键提示)。
// 支持多选(space 勾选 + enter 确认)、数字 1-9 直选、ctrl+u/w 清词。
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Candidate 是待选条目。
type Candidate struct {
	Value string // 选中值(过滤与勾选都以它为基准)
	Desc  string // 展示用描述(灰色,置于 Value 之后)
}

// Options 配置过滤选择器 TUI。
type Options struct {
	Title      string      // 标题栏主文本(绿色粗体)
	Subtitle   string      // 标题栏次要文本(灰色,可选)
	Cands      []Candidate // 全量候选
	Query      string      // 预填查询
	Initial    int         // 初始光标(默认 0)
	JumpKeys   bool        // 允许数字 1-9 直选(无输入时生效)
	StatusHint string      // 状态栏快捷键提示
	Multi      bool        // 多选模式:space 切换勾选,enter 确认全部勾选项
	Selected   []string    // 多选模式初始勾选的 Value 列表
}

// Result 是选择器的返回结果。
type Result struct {
	Value     string   // 单选模式:选中值
	Selected  []string // 多选模式:勾选的 Value 列表(按 Cands 顺序)
	Index     int
	Cancelled bool // esc/ctrl+c 取消,或过滤结果为空
}

// Run 在终端上运行选择器并返回结果。
//
// 调用方负责传入输入/输出文件(stdout 留给执行结果输出,故此处显式分开)
// 并处理非交互退化;本函数不做 TTY 检测。in/out 通常来自 /dev/tty。
func Run(opts Options, in, out *os.File) (Result, error) {
	// 强制 TrueColor,绕开 lipgloss 基于 os.Stdout 的自动探测:
	// stdout 常被命令替换捕获成管道,自动探测会判定为无色。
	lipgloss.SetColorProfile(termenv.TrueColor)

	initial := opts.Initial
	m := newModel(opts, initial)
	p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out))
	final, err := p.Run()
	if err != nil {
		return Result{}, err
	}
	res := final.(model)
	if res.cancelled || len(res.filtered) == 0 {
		return Result{Cancelled: true}, nil
	}
	if opts.Multi {
		return Result{Selected: res.selectedValues(opts.Cands), Index: res.cursor}, nil
	}
	return Result{Value: res.filtered[res.cursor].Value, Index: res.cursor}, nil
}

// ---- 样式(与 shr/tui 同款) ----

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("120")) // 标题(绿粗体)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))            // 次要文本(灰)
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("36")).Bold(true)  // 计数(青绿)
	hlStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("213"))            // 命中片段(粉色,非光标行)
	curStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("120")) // 光标行:整行绿色粗体
	sepStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("237"))            // 分隔线(浅灰)
	promptStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("120")) // 输入提示符 ❯
)

// tickMsg 驱动输入光标闪烁(每 500ms 一次)。
type tickMsg time.Time

// model 是过滤选择器的 bubbletea 模型。
// 手写文本输入(append-only query + backspace/ctrl+u/ctrl+w)与视口滚动,无 bubbles 依赖。
type model struct {
	opts      Options
	filtered  []Candidate // 当前过滤结果
	query     string      // 查询字符串(append-only,光标始终在末尾)
	cursor    int         // 列表光标位置
	scrollY   int         // 视口滚动位置(首个可见行索引)
	height    int         // 终端高度(tea.WindowSizeMsg)
	width     int         // 终端宽度(tea.WindowSizeMsg),用于分隔线与反色行延伸
	phase     int         // 光标闪烁相位(0=显示 ▏,1=隐藏)
	cancelled bool
	selected  map[string]bool // 多选模式勾选集合(Value → true)
}

func newModel(opts Options, initial int) model {
	m := model{opts: opts, query: opts.Query, cursor: initial}
	if m.cursor < 0 || m.cursor >= len(opts.Cands) {
		m.cursor = 0
	}
	if opts.Multi {
		m.selected = make(map[string]bool, len(opts.Selected))
		for _, v := range opts.Selected {
			m.selected[v] = true
		}
	}
	m.recompute()
	return m
}

// selectedValues 按 Cands 顺序返回勾选的 Value 列表(多选模式)。
func (m model) selectedValues(cands []Candidate) []string {
	if len(m.selected) == 0 {
		return nil
	}
	var out []string
	for _, c := range cands {
		if m.selected[c.Value] {
			out = append(out, c.Value)
		}
	}
	return out
}

// recompute 根据当前 query 重新过滤,保持光标在有效范围内。
func (m *model) recompute() {
	m.filtered = filterCands(m.opts.Cands, m.query)
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.ensureVisible()
}

func (m model) Init() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
		m.ensureVisible()
		return m, nil
	case tickMsg:
		m.phase ^= 1
		return m, tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			if len(m.filtered) > 0 {
				return m, tea.Quit
			}
		case " ":
			if m.opts.Multi && m.cursor < len(m.filtered) {
				v := m.filtered[m.cursor].Value
				if m.selected[v] {
					delete(m.selected, v)
				} else {
					m.selected[v] = true
				}
			}
		case "up", "k", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
				m.ensureVisible()
			}
		case "down", "j", "ctrl+n", "tab":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
				m.ensureVisible()
			}
		// 数字 1-9 直选(菜单用):无输入时跳过逐行移动快速选中。
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if m.opts.JumpKeys && m.query == "" {
				n := int(msg.String()[0] - '0')
				if n-1 < len(m.filtered) {
					m.cursor = n - 1
					return m, tea.Quit
				}
			} else {
				m.query += msg.String()
				m.recompute()
			}
		case "backspace":
			if len(m.query) > 0 {
				r := []rune(m.query)
				m.query = string(r[:len(r)-1])
				m.recompute()
			}
		case "ctrl+u":
			m.query = ""
			m.recompute()
		case "ctrl+w":
			fields := strings.Fields(m.query)
			if len(fields) > 0 {
				m.query = strings.Join(fields[:len(fields)-1], " ")
			} else {
				m.query = ""
			}
			m.recompute()
		default:
			s := msg.String()
			if isPrintableKey(s) {
				m.query += s
				m.recompute()
			}
		}
	}
	return m, nil
}

// isPrintableKey 判断 bubbletea KeyMsg 字符串是否为可打印字符(单 rune,非控制键)。
func isPrintableKey(s string) bool {
	runes := []rune(s)
	if len(runes) != 1 {
		return false
	}
	r := runes[0]
	return r >= 32 && r != 127
}

// ensureVisible 调整视口使光标可见。
// 固定行数:标题(1) + 分隔线(1) + [列表] + 分隔线(1) + 输入行(1) + 状态栏(1) = 5
func (m *model) ensureVisible() {
	const fixedLines = 5
	visibleRows := m.height - fixedLines
	if visibleRows <= 0 {
		visibleRows = 10 // WindowSizeMsg 尚未到达时的默认值
	}
	if m.cursor < m.scrollY {
		m.scrollY = m.cursor
	}
	if m.cursor >= m.scrollY+visibleRows {
		m.scrollY = m.cursor - visibleRows + 1
	}
}

func (m model) View() string {
	const fixedLines = 5
	visibleRows := m.height - fixedLines
	if visibleRows <= 0 {
		visibleRows = 10
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	sep := sepStyle.Render(strings.Repeat("─", width))

	var b strings.Builder
	// 标题栏
	b.WriteString(titleStyle.Render(m.opts.Title))
	if m.opts.Subtitle != "" {
		b.WriteString(dimStyle.Render("  ·  " + m.opts.Subtitle))
	}
	b.WriteString("\n")
	b.WriteString(sep)
	b.WriteString("\n")

	// 列表(视口区间)
	end := m.scrollY + visibleRows
	if end > len(m.filtered) {
		end = len(m.filtered)
	}
	for i := m.scrollY; i < end; i++ {
		c := m.filtered[i]
		box := ""
		if m.opts.Multi {
			if m.selected[c.Value] {
				box = okStyle.Render("[x] ")
			} else {
				box = "[ ] "
			}
		}
		if i == m.cursor {
			// 光标行:整行绿色粗体(不再嵌套命中高亮,避免内层 \x1b[0m 重置外层颜色)
			line := "  ▸ " + box + c.Value
			if c.Desc != "" {
				line += "  " + c.Desc
			}
			if pad := width - lipgloss.Width(line); pad > 0 {
				line += strings.Repeat(" ", pad)
			}
			b.WriteString(curStyle.Render(line))
		} else {
			b.WriteString("    " + box + renderCandidateLine(c, m.query, width))
		}
		b.WriteString("\n")
	}
	if len(m.filtered) == 0 {
		b.WriteString(dimStyle.Render("    (no matches)"))
		b.WriteString("\n")
	}

	// 分隔线 + 输入行 + 状态栏
	b.WriteString(sep)
	b.WriteString("\n")
	cursorBlock := "▏"
	if m.phase == 1 {
		cursorBlock = " "
	}
	b.WriteString(promptStyle.Render("❯ ") + m.query + cursorBlock)
	b.WriteString("\n")
	count := fmt.Sprintf("%d/%d", len(m.filtered), len(m.opts.Cands))
	b.WriteString("  " + okStyle.Render(count))
	if m.opts.StatusHint != "" {
		b.WriteString(dimStyle.Render("  ·  " + m.opts.StatusHint))
	}
	b.WriteString("\n")

	return b.String()
}

// renderCandidateLine 渲染候选行:value 命中高亮 + 灰色描述(按显示宽度截断)。
func renderCandidateLine(c Candidate, query string, width int) string {
	value := renderHighlighted(c.Value, query)
	if c.Desc == "" {
		return value
	}
	// 描述仅占 value 之后的剩余宽度(减 2 个分隔空格),太窄就不展示
	remaining := width - lipgloss.Width(value) - 2
	if remaining < 4 {
		return value
	}
	return value + "  " + dimStyle.Render(truncateWidth(c.Desc, remaining))
}

// truncateWidth 按显示宽度截断 s 至 maxWidth,超长末尾补 …。
func truncateWidth(s string, maxWidth int) string {
	if maxWidth <= 1 {
		return "…"
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	var b strings.Builder
	w := 0
	for _, r := range runes {
		rw := lipgloss.Width(string(r))
		if w+rw > maxWidth-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// ---- 过滤与高亮(本地实现:空格分隔 token 全部子串匹配,大小写不敏感) ----

// filterCands 返回 query 的所有 token 都在 Value 中出现过的候选。
func filterCands(cands []Candidate, query string) []Candidate {
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return cands
	}
	var out []Candidate
	for _, c := range cands {
		if matchAll(strings.ToLower(c.Value), tokens) {
			out = append(out, c)
		}
	}
	return out
}

func tokenize(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	out := fields[:0]
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func matchAll(s string, tokens []string) bool {
	for _, t := range tokens {
		if !strings.Contains(s, t) {
			return false
		}
	}
	return true
}

// highlightRanges 返回 query token 在 s 中的命中区间(rune 索引对,升序)。
func highlightRanges(s, query string) [][2]int {
	var ranges [][2]int
	runes := []rune(strings.ToLower(s))
	for _, tok := range tokenize(query) {
		needle := []rune(tok)
		from := 0
		for {
			i := indexRunes(runes, needle, from)
			if i < 0 {
				break
			}
			ranges = append(ranges, [2]int{i, i + len(needle)})
			from = i + len(needle)
		}
	}
	return ranges
}

func indexRunes(hay, needle []rune, from int) int {
	if len(needle) == 0 || from > len(hay) {
		return -1
	}
	for i := from; i+len(needle) <= len(hay); i++ {
		matched := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	return -1
}

// renderHighlighted 渲染候选字符串,高亮匹配片段(粉色)。
func renderHighlighted(s, query string) string {
	ranges := highlightRanges(s, query)
	if len(ranges) == 0 {
		return s
	}
	runes := []rune(s)
	var b strings.Builder
	pos := 0
	for _, r := range ranges {
		if pos < r[0] {
			b.WriteString(string(runes[pos:r[0]]))
		}
		b.WriteString(hlStyle.Render(string(runes[r[0]:r[1]])))
		pos = r[1]
	}
	if pos < len(runes) {
		b.WriteString(string(runes[pos:]))
	}
	return b.String()
}
