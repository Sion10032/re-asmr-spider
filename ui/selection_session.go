package ui

import (
	"strings"

	"github.com/charmbracelet/bubbletea"

	"re-asmr-spider/app"
	"re-asmr-spider/i18n"
	"re-asmr-spider/utils"
)

// SelectionProvider 选择会话对业务层的钩子抽象，由 app.SelectionSession 实现。
type SelectionProvider interface {
	// Confirm 确认选择：落盘并建立策略，返回落盘错误由 TUI 呈现
	Confirm(prep app.TaskPreparation, keys map[string]bool) error
	// Cancel 用户中止时清理（停止监控等）
	Cancel()
	// Finish 全部任务落定后执行下载并收尾，返回流程是否成功
	Finish(waitForInput bool) bool
}

// sessionModel 多作品树选择会话：任务在会话外预解析（拉取文件树与沿用判定），
// 会话仅包含需要手动选择的任务。单个全屏程序内依次完成各作品的勾选，
// 无加载态、作品间切换零回显。
type sessionModel struct {
	prov      SelectionProvider
	preps     []app.TaskPreparation // 仅含需要选择的任务（预解析结果）
	index     int                   // 当前任务下标
	prep      app.TaskPreparation   // 当前任务的前置数据
	tree      *treeSelectorModel    // 当前任务的委托模型
	summary   []string              // 跳过/落盘失败等提示（退出后由调用方打印）
	skipArmed bool                  // 全不选时已按过一次回车（二次确认跳过该作品）
	width     int
	height    int
	aborted   bool // 用户中止（q/Esc）
	done      bool // 全部任务处理完毕
}

// newSessionModel 构造会话模型
func newSessionModel(prov SelectionProvider, preps []app.TaskPreparation) *sessionModel {
	return &sessionModel{prov: prov, preps: preps}
}

// enterTree 进入指定任务的树选择态
func (m *sessionModel) enterTree(prep app.TaskPreparation) {
	m.prep = prep
	m.tree = newTreeSelectorModel(prep.BasePath, prep.Tracks, prep.InitialKeys)
	m.skipArmed = false
	m.forwardSize()
}

// forwardSize 把窗口尺寸转发给委托的树模型：会话进度行多占一行，高度减一
func (m *sessionModel) forwardSize() {
	if m.tree == nil || m.width <= 0 || m.height <= 0 {
		return
	}
	next, _ := m.tree.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height - 1})
	if t, ok := next.(*treeSelectorModel); ok {
		m.tree = t
	}
}

// advance 前进到下一个任务；全部处理完毕时退出程序
func (m *sessionModel) advance() tea.Cmd {
	m.index++
	m.tree = nil
	m.prep = app.TaskPreparation{}
	m.skipArmed = false
	if m.index >= len(m.preps) {
		m.done = true
		return tea.Quit
	}
	m.enterTree(m.preps[m.index])
	return nil
}

// Init 实现 tea.Model：直接进入首个任务的树选择（任务已预解析，无需加载态）
func (m *sessionModel) Init() tea.Cmd {
	if len(m.preps) == 0 {
		m.done = true
		return tea.Quit
	}
	m.enterTree(m.preps[0])
	return nil
}

// Update 处理窗口尺寸与按键消息
func (m *sessionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.forwardSize()
		return m, nil

	case tea.KeyMsg:
		if m.tree == nil {
			return m, nil
		}
		// 全不选时回车：拦截为「二次确认跳过该作品」，既防误触也避免用户卡死在树中
		if msg.String() == "enter" && m.tree.totalSelected() == 0 {
			if !m.skipArmed {
				m.skipArmed = true
				m.tree.statusMsg = i18n.T("session_skip_confirm")
				return m, nil
			}
			m.summary = append(m.summary, i18n.T("selection_task_skipped", m.prep.Task))
			return m, m.advance()
		}
		if msg.String() != "enter" {
			m.skipArmed = false
		}
		// 委托给树模型；丢弃其返回的命令（退出由标志位驱动，避免委托的 tea.Quit 直接杀掉会话）
		next, _ := m.tree.Update(msg)
		if t, ok := next.(*treeSelectorModel); ok {
			m.tree = t
		}
		switch {
		case m.tree.confirmed:
			keys := m.tree.selected
			if len(keys) == 0 {
				// 防御路径（回车已被上方拦截）：按跳过处理
				m.summary = append(m.summary, i18n.T("selection_task_skipped", m.prep.Task))
				return m, m.advance()
			}
			if err := m.prov.Confirm(m.prep, keys); err != nil {
				// 落盘失败：记录提示（退出全屏后打印），策略已建立，继续推进
				m.summary = append(m.summary, i18n.T("selection_save_failed", m.prep.Task, err))
			}
			return m, m.advance()
		case m.tree.cancelled:
			// q/Esc 放弃选择：中止整个流程
			m.aborted = true
			return m, tea.Quit
		}
		return m, nil
	}
	return m, nil
}

// View 渲染整屏：顶部会话进度行，其余委托树视图
func (m *sessionModel) View() string {
	if m.width == 0 || m.height == 0 || m.tree == nil {
		return i18n.T("tui_loading")
	}
	progress := i18n.T("session_progress", m.index+1, len(m.preps), m.prep.Task)
	var b strings.Builder
	b.WriteString(truncateWidth(progress, m.width))
	b.WriteString("\n")
	b.WriteString(m.tree.View())
	return b.String()
}

// RunSelectionSession 运行树选择会话：仅包含需要手动选择的任务，全程单个全屏程序。
// 返回 proceed 表示是否完成（false=用户中止 q/Esc 或程序异常，此时已调用 prov.Cancel）；
// notices 为会话内的跳过/落盘失败等提示，由调用方在退出全屏后打印。
func RunSelectionSession(prov SelectionProvider, preps []app.TaskPreparation) (proceed bool, notices []string) {
	if len(preps) == 0 {
		return true, nil
	}

	// 会话期间静音全局日志：spider 层的错误输出会破坏全屏画面，改由 TUI 状态呈现
	utils.SetMuted(true)
	defer utils.SetMuted(false)

	m := newSessionModel(prov, preps)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		prov.Cancel()
		return false, nil
	}
	sm, ok := final.(*sessionModel)
	if !ok {
		prov.Cancel()
		return false, nil
	}
	if sm.aborted {
		prov.Cancel()
		return false, nil
	}
	return true, sm.summary
}
