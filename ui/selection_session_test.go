package ui

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"

	"re-asmr-spider/app"
	"re-asmr-spider/i18n"
	"re-asmr-spider/spider"
)

// TestMain 加载 i18n 翻译，使会话模型的消息格式化在测试中可用
func TestMain(m *testing.M) {
	if err := i18n.Init("zh-CN"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// fakeProvider 会话业务钩子的测试替身：记录调用并模拟落盘失败。
type fakeProvider struct {
	confirmErr error    // Confirm 返回的错误（模拟落盘失败）
	confirmed  []string // Confirm 调用顺序
	cancelled  bool
}

func (f *fakeProvider) Confirm(prep app.TaskPreparation, keys map[string]bool) error {
	f.confirmed = append(f.confirmed, prep.Task)
	return f.confirmErr
}

func (f *fakeProvider) Cancel() { f.cancelled = true }

func (f *fakeProvider) Finish(waitForInput bool) bool { return true }

// treePrep 构造需要树选择的任务（复用 tree_selector_test 的 3 叶子树）。
func treePrep(task string, initial map[string]bool) app.TaskPreparation {
	return app.TaskPreparation{
		Task:        task,
		BasePath:    "downloads/" + task,
		Tracks:      buildTestTree(),
		Analysis:    &spider.FormatAnalysis{},
		InitialKeys: initial,
	}
}

// sendMsg 向模型发送按键等消息。
func sendMsg(t *testing.T, m *sessionModel, msg tea.Msg) *sessionModel {
	t.Helper()
	next, _ := m.Update(msg)
	nm, ok := next.(*sessionModel)
	if !ok {
		t.Fatal("Update 返回了非 *sessionModel 类型")
	}
	return nm
}

// startSession 构造会话模型并完成 Init 与初始尺寸设定（树选择态就绪）。
func startSession(t *testing.T, prov *fakeProvider, preps []app.TaskPreparation) *sessionModel {
	t.Helper()
	m := newSessionModel(prov, preps)
	m.Init()
	return sendMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 12})
}

// TestSessionTreeConfirm 多任务依次树选择：确认后自动前进到下一个，全部完成后结束。
func TestSessionTreeConfirm(t *testing.T) {
	prov := &fakeProvider{}
	m := startSession(t, prov, []app.TaskPreparation{
		treePrep("RJ1", nil), // nil → 全选
		treePrep("RJ2", nil),
	})

	if m.tree == nil || m.prep.Task != "RJ1" {
		t.Fatalf("应先进入 RJ1 的树选择, got %v", m.prep.Task)
	}

	// enter 确认 RJ1（全选状态直接通过），自动前进到 RJ2
	m = sendMsg(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.tree == nil || m.prep.Task != "RJ2" {
		t.Fatalf("确认后应前进到 RJ2 的树选择, got %v", m.prep.Task)
	}

	// enter 确认 RJ2，全部完成
	m = sendMsg(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if !m.done {
		t.Fatal("全部确认后会话应完成")
	}
	if len(prov.confirmed) != 2 || prov.confirmed[0] != "RJ1" || prov.confirmed[1] != "RJ2" {
		t.Fatalf("Confirm 调用 = %v, 期望 [RJ1 RJ2]", prov.confirmed)
	}
}

// TestSessionTreeCancel 树选择中 q 退出：会话标记中止，不调用 Confirm。
func TestSessionTreeCancel(t *testing.T) {
	prov := &fakeProvider{}
	m := startSession(t, prov, []app.TaskPreparation{treePrep("RJ1", nil)})

	m = sendMsg(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if !m.aborted {
		t.Fatal("树选择中 q 应中止会话")
	}
	if m.done {
		t.Fatal("中止不应标记完成")
	}
	if len(prov.confirmed) != 0 {
		t.Fatalf("中止不应有 Confirm 调用, 实际 %v", prov.confirmed)
	}
}

// TestSessionEmptySelectionSkips 全不选时回车：第一次仅提示，第二次确认跳过该作品（不调用 Confirm）。
func TestSessionEmptySelectionSkips(t *testing.T) {
	prov := &fakeProvider{}
	m := startSession(t, prov, []app.TaskPreparation{treePrep("RJ1", map[string]bool{})})

	// 第一次回车：进入跳过确认 armed 态，不退出
	m = sendMsg(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.done || m.aborted {
		t.Fatal("第一次回车不应退出")
	}
	if len(prov.confirmed) != 0 {
		t.Fatalf("第一次回车不应调用 Confirm, 实际 %v", prov.confirmed)
	}

	// 第二次回车：确认跳过
	m = sendMsg(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if !m.done {
		t.Fatal("二次确认后跳过并完成")
	}
	if len(prov.confirmed) != 0 {
		t.Fatalf("跳过不应调用 Confirm, 实际 %v", prov.confirmed)
	}
	if !strings.Contains(strings.Join(m.summary, "\n"), "RJ1") {
		t.Fatal("摘要应包含跳过提示")
	}
}

// TestSessionSaveError 落盘失败：提示被记录（退出后由调用方打印）但仍推进完成。
func TestSessionSaveError(t *testing.T) {
	prov := &fakeProvider{confirmErr: strconv.ErrSyntax}
	m := startSession(t, prov, []app.TaskPreparation{treePrep("RJ1", nil)})

	m = sendMsg(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if !m.done {
		t.Fatal("落盘失败后仍应推进完成")
	}
	if len(prov.confirmed) != 1 {
		t.Fatal("落盘失败仍应记录 Confirm 调用")
	}
	if !strings.Contains(strings.Join(m.summary, "\n"), "RJ1") {
		t.Fatal("摘要应包含落盘失败提示")
	}
}

// TestSessionSizeForward 会话比树视图多占一行：窗口尺寸转发时高度应减一。
func TestSessionSizeForward(t *testing.T) {
	prov := &fakeProvider{}
	m := newSessionModel(prov, []app.TaskPreparation{treePrep("RJ1", nil)})
	m.Init()

	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	m = next.(*sessionModel)

	if m.tree == nil || m.tree.height != 11 || m.tree.width != 80 {
		t.Fatalf("树模型应收到高度减一的尺寸, got %dx%d", m.tree.width, m.tree.height)
	}
}

// TestSessionView 树选择态的画面要素：进度行、作品号与展开标识。
func TestSessionView(t *testing.T) {
	prov := &fakeProvider{}
	m := startSession(t, prov, []app.TaskPreparation{treePrep("RJ1", nil)})

	view := m.View()
	if !strings.Contains(view, "1/1") || !strings.Contains(view, "RJ1") {
		t.Fatalf("树态画面应包含进度与作品号, 实际:\n%s", view)
	}
	if !strings.Contains(view, "] > ") {
		t.Fatal("树态画面应包含文件夹折叠指示符")
	}
}
