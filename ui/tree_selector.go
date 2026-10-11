package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"re-asmr-spider/spider"
)

// selectorNode 文件树选择器的节点
type selectorNode struct {
	Key      string          // 唯一标识，经 spider.TrackKey 计算
	Title    string          // 显示标题
	IsFolder bool            // 是否为目录
	Children []*selectorNode // 子节点（仅目录有）
}

// buildNodes 将 spider.Track 树转换为选择器节点树；
// parentPath 为当前层级的目录路径，文件的 key 使用其所在目录路径计算。
func buildNodes(tree []spider.Track, parentPath string) []*selectorNode {
	nodes := make([]*selectorNode, 0, len(tree))
	for i := range tree {
		t := &tree[i]
		if t.Type == "folder" {
			nodes = append(nodes, &selectorNode{
				Key:      spider.TrackKey(t.Hash, parentPath, t.Title),
				Title:    t.Title,
				IsFolder: true,
				Children: buildNodes(t.Children, filepath.Join(parentPath, t.Title)),
			})
		} else {
			nodes = append(nodes, &selectorNode{
				Key:   spider.TrackKey(t.Hash, parentPath, t.Title),
				Title: t.Title,
			})
		}
	}
	return nodes
}

// collectFileInfos 汇总树中全部文件（含完整路径），用于格式统计。
func collectFileInfos(tree []spider.Track, parentPath string) []*spider.FileInfo {
	files := make([]*spider.FileInfo, 0)
	for i := range tree {
		t := &tree[i]
		if t.Type == "folder" {
			files = append(files, collectFileInfos(t.Children, filepath.Join(parentPath, t.Title))...)
		} else {
			files = append(files, &spider.FileInfo{
				Track: t,
				Path:  filepath.Join(parentPath, t.Title),
			})
		}
	}
	return files
}

// initSelectedKeys 计算初始选中集合：initialKeys 非 nil 时恰为该集合的拷贝，nil 时全选。
func initSelectedKeys(nodes []*selectorNode, initialKeys map[string]bool) map[string]bool {
	if initialKeys != nil {
		selected := make(map[string]bool, len(initialKeys))
		for k, v := range initialKeys {
			selected[k] = v
		}
		return selected
	}
	selected := make(map[string]bool)
	var walk func(ns []*selectorNode)
	walk = func(ns []*selectorNode) {
		for _, n := range ns {
			if n.IsFolder {
				walk(n.Children)
			} else {
				selected[n.Key] = true
			}
		}
	}
	walk(nodes)
	return selected
}

// toggleNode 切换节点勾选状态：叶子直接反转；目录递归整树切换——
// 子树当前已全部选中则全部取消，否则全部选中。
func toggleNode(keys map[string]bool, n *selectorNode) {
	if n == nil {
		return
	}
	if !n.IsFolder {
		if keys[n.Key] {
			delete(keys, n.Key)
		} else {
			keys[n.Key] = true
		}
		return
	}
	selected, total := countNode(n, keys)
	target := true
	if total > 0 && selected == total {
		target = false
	}
	setSubtreeSelected(keys, n, target)
}

// setSubtreeSelected 将节点子树内全部叶子置为 target 状态。
func setSubtreeSelected(keys map[string]bool, n *selectorNode, target bool) {
	if !n.IsFolder {
		if target {
			keys[n.Key] = true
		} else {
			delete(keys, n.Key)
		}
		return
	}
	for _, c := range n.Children {
		setSubtreeSelected(keys, c, target)
	}
}

// countNode 统计节点子树内（叶子）已选/总数。
func countNode(n *selectorNode, keys map[string]bool) (int, int) {
	if n == nil {
		return 0, 0
	}
	if !n.IsFolder {
		if keys[n.Key] {
			return 1, 1
		}
		return 0, 1
	}
	var selected, total int
	for _, c := range n.Children {
		s, t := countNode(c, keys)
		selected += s
		total += t
	}
	return selected, total
}

// pageCursor 按页步长移动光标并夹紧到 [0, total-1]：
// delta 为负向上翻页，为正向下翻页，步长通常为 listHeight()。
func pageCursor(cursor, delta, total int) int {
	if total <= 0 {
		return 0
	}
	next := cursor + delta
	if next > total-1 {
		next = total - 1
	}
	if next < 0 {
		next = 0
	}
	return next
}

// flatRow 可视平铺列表中的一行：节点 + 层级深度。
type flatRow struct {
	node  *selectorNode
	depth int
}

// treeSelectorModel 文件树勾选器的 bubbletea 模型
type treeSelectorModel struct {
	rjID      string
	roots     []*selectorNode
	flat      []flatRow
	depths    map[*selectorNode]int
	selected  map[string]bool
	expanded  map[string]bool
	cursor    int
	offset    int
	width     int
	height    int
	fileInfos []*spider.FileInfo
	confirmed bool
	cancelled bool
	statusMsg string
}

// newTreeSelectorModel 构造初始模型
func newTreeSelectorModel(rjID string, tree []spider.Track, initialKeys map[string]bool) *treeSelectorModel {
	roots := buildNodes(tree, "")
	m := &treeSelectorModel{
		rjID:      rjID,
		roots:     roots,
		depths:    make(map[*selectorNode]int),
		selected:  initSelectedKeys(roots, initialKeys),
		expanded:  make(map[string]bool),
		fileInfos: collectFileInfos(tree, ""),
	}
	m.refreshFlat()
	return m
}

// refreshFlat 依据折叠状态重新生成可视平铺列表
func (m *treeSelectorModel) refreshFlat() {
	m.flat = m.flat[:0]
	var walk func(ns []*selectorNode, depth int)
	walk = func(ns []*selectorNode, depth int) {
		for _, n := range ns {
			m.flat = append(m.flat, flatRow{node: n, depth: depth})
			if n.IsFolder && m.expanded[n.Key] {
				walk(n.Children, depth+1)
			}
		}
	}
	walk(m.roots, 0)
	if m.cursor >= len(m.flat) {
		m.cursor = len(m.flat) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// syncOffset 滚动同步：使 offset 窗口跟随光标，保证光标始终在可视区内，
// 并夹紧 offset 到合法范围。仅由 Update 调用，View 保持纯渲染。
func (m *treeSelectorModel) syncOffset() {
	listH := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+listH {
		m.offset = m.cursor - listH + 1
	}
	if m.offset > len(m.flat)-1 {
		m.offset = len(m.flat) - 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// listHeight 可视区可容纳的行数（顶部两行 + 底部两行固定）
func (m *treeSelectorModel) listHeight() int {
	h := m.height - 4
	if h < 1 {
		h = 1
	}
	return h
}

// totalSelected 统计整树已选文件数
func (m *treeSelectorModel) totalSelected() int {
	n := 0
	for _, r := range m.roots {
		s, _ := countNode(r, m.selected)
		n += s
	}
	return n
}

// Init 实现 tea.Model，无需启动命令
func (m *treeSelectorModel) Init() tea.Cmd {
	return nil
}

// Update 处理按键与窗口尺寸消息
func (m *treeSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncOffset()
		return m, nil

	case tea.KeyMsg:
		m.statusMsg = ""
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.syncOffset()
			}
		case "down", "j":
			if m.cursor < len(m.flat)-1 {
				m.cursor++
				m.syncOffset()
			}
		case " ":
			if m.cursor < len(m.flat) {
				toggleNode(m.selected, m.flat[m.cursor].node)
			}
		case "enter", "right":
			if m.cursor < len(m.flat) {
				n := m.flat[m.cursor].node
				if n.IsFolder {
					m.expanded[n.Key] = true
					m.refreshFlat()
					m.syncOffset()
				}
			}
		case "left":
			if m.cursor < len(m.flat) {
				n := m.flat[m.cursor].node
				if n.IsFolder && m.expanded[n.Key] {
					m.expanded[n.Key] = false
					m.refreshFlat()
					m.syncOffset()
				}
			}
		case "a":
			for _, r := range m.roots {
				setSubtreeSelected(m.selected, r, true)
			}
			m.statusMsg = "已全选"
		case "n":
			for _, r := range m.roots {
				setSubtreeSelected(m.selected, r, false)
			}
			m.statusMsg = "已全不选"
		case "pgup":
			m.cursor = pageCursor(m.cursor, -m.listHeight(), len(m.flat))
			m.syncOffset()
		case "pgdown":
			m.cursor = pageCursor(m.cursor, m.listHeight(), len(m.flat))
			m.syncOffset()
		case "q":
			if m.totalSelected() == 0 {
				m.statusMsg = "未选择任何文件"
				return m, nil
			}
			m.confirmed = true
			return m, tea.Quit
		case "esc":
			m.cancelled = true
			return m, tea.Quit
		}
		return m, nil
	}
	return m, nil
}

// truncateWidth 按终端显示列宽截断字符串（CJK 等全角字符占 2 列），
// 超出部分以省略号（宽 1 列）结尾，保证结果渲染宽度不超过 max。
func truncateWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return runewidth.Truncate(s, max, "…")
}

// View 渲染整屏：顶部固定 RJ 号与统计行，中部可视区滚动列表，底部状态与按键提示
func (m *treeSelectorModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "正在加载文件树选择器..."
	}

	listH := m.listHeight()

	stats := spider.CountByFormat(m.fileInfos)
	header := fmt.Sprintf("RJ%s", m.rjID)
	statsLine := fmt.Sprintf("已选 %d | wav:%d flac:%d mp3:%d aac:%d 其它:%d | 共 %d 文件",
		m.totalSelected(), stats.Wav, stats.Flac, stats.Mp3, stats.Aac, stats.Other, stats.Total)

	var b strings.Builder
	b.WriteString(truncateWidth(header, m.width))
	b.WriteString("\n")
	b.WriteString(truncateWidth(statsLine, m.width))
	b.WriteString("\n")

	end := m.offset + listH
	if end > len(m.flat) {
		end = len(m.flat)
	}
	for i := m.offset; i < end; i++ {
		row := m.flat[i]
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}
		indent := strings.Repeat("  ", row.depth)

		var line string
		if row.node.IsFolder {
			mark := "[ ]"
			sel, total := countNode(row.node, m.selected)
			switch {
			case total > 0 && sel == total:
				mark = "[x]"
			case sel > 0:
				mark = "[-]"
			}
			line = fmt.Sprintf("%s%s%s %s (%d/%d)", prefix, indent, mark, row.node.Title, sel, total)
		} else {
			mark := "[ ]"
			if m.selected[row.node.Key] {
				mark = "[x]"
			}
			line = fmt.Sprintf("%s%s%s %s", prefix, indent, mark, row.node.Title)
		}
		b.WriteString(truncateWidth(line, m.width))
		b.WriteString("\n")
	}

	// 补齐可视区空行，保证底部状态行位置稳定
	for i := end - m.offset; i < listH; i++ {
		b.WriteString("\n")
	}

	status := m.statusMsg
	if status == "" {
		status = " "
	}
	b.WriteString(truncateWidth(status, m.width))
	b.WriteString("\n")

	hints := "↑/↓/j/k 移动  空格 勾选  回车/→ 展开 ← 折叠  a 全选  n 全不选  PgUp/PgDn 滚动  q 确认  Esc 取消"
	b.WriteString(truncateWidth(hints, m.width))
	return b.String()
}

// RunTreeSelector 启动全屏文件树勾选器。
// initialKeys 非 nil 时初始选中恰为该集合，nil 时全选；
// confirmed=false 表示 Esc 取消，此时 selectedKeys 返回 nil。
func RunTreeSelector(rjID string, tree []spider.Track, initialKeys map[string]bool) (selectedKeys map[string]bool, confirmed bool) {
	model := newTreeSelectorModel(rjID, tree, initialKeys)
	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return nil, false
	}
	m, ok := finalModel.(*treeSelectorModel)
	if !ok {
		return nil, false
	}
	if !m.confirmed {
		return nil, false
	}
	return m.selected, true
}
