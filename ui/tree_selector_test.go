package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"re-asmr-spider/spider"
)

// buildTestTree 构造测试用的曲目树：
// 文件夹A（3 个文件）
// ├── 01.wav (hash: h1)
// └── 子目录
//
//	├── 02.mp3 (hash: h2)
//	└── 03.flac (hash: h3)
//
// no-hash.txt（无 hash，key 回退为 path|title）
// 04.m4a (hash: h4)
func buildTestTree() []spider.Track {
	return []spider.Track{
		{
			Type:  "folder",
			Title: "文件夹A",
			Children: []spider.Track{
				{Type: "audio", Title: "01.wav", Hash: "h1"},
				{
					Type:  "folder",
					Title: "子目录",
					Children: []spider.Track{
						{Type: "audio", Title: "02.mp3", Hash: "h2"},
						{Type: "audio", Title: "03.flac", Hash: "h3"},
					},
				},
			},
		},
		{Type: "text", Title: "no-hash.txt"},
		{Type: "audio", Title: "04.m4a", Hash: "h4"},
	}
}

// leafKeysOf 汇总节点树中全部叶子（文件）的 key。
func leafKeysOf(nodes []*selectorNode) map[string]bool {
	keys := make(map[string]bool)
	var walk func(ns []*selectorNode)
	walk = func(ns []*selectorNode) {
		for _, n := range ns {
			if n.IsFolder {
				walk(n.Children)
			} else {
				keys[n.Key] = true
			}
		}
	}
	walk(nodes)
	return keys
}

// TestBuildNodesSortedOrder 验证每层节点排序：文件夹在前、文件在后，组内按标题默认字典序。
func TestBuildNodesSortedOrder(t *testing.T) {
	tree := []spider.Track{
		{Type: "audio", Title: "10.mp3", Hash: "h10"},
		{Type: "folder", Title: "B盘", Children: []spider.Track{
			{Type: "audio", Title: "track10.wav", Hash: "h10b"},
			{Type: "audio", Title: "track2.wav", Hash: "h2"},
		}},
		{Type: "audio", Title: "02.mp3", Hash: "h02"},
		{Type: "folder", Title: "A盘", Children: []spider.Track{}},
		{Type: "audio", Title: "2.mp3", Hash: "h2x"},
	}
	nodes := buildNodes(tree, "base")

	if len(nodes) != 5 {
		t.Fatalf("根节点数 = %d, 期望 5", len(nodes))
	}

	// 文件夹在前，组内按默认字典序：A盘 < B盘
	if !nodes[0].IsFolder || nodes[0].Title != "A盘" {
		t.Fatalf("nodes[0] = %+v, 期望文件夹 A盘", nodes[0])
	}
	if !nodes[1].IsFolder || nodes[1].Title != "B盘" {
		t.Fatalf("nodes[1] = %+v, 期望文件夹 B盘", nodes[1])
	}

	// 文件组按默认字典序：02.mp3 < 10.mp3 < 2.mp3
	got := []string{nodes[2].Title, nodes[3].Title, nodes[4].Title}
	want := []string{"02.mp3", "10.mp3", "2.mp3"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("文件顺序 = %v, 期望 %v", got, want)
		}
	}

	// 子层级同样生效：字典序 track10 < track2
	children := nodes[1].Children
	if children[0].Title != "track10.wav" || children[1].Title != "track2.wav" {
		t.Fatalf("子级顺序 = [%s %s], 期望 [track10.wav track2.wav]", children[0].Title, children[1].Title)
	}
}

func TestBuildNodes(t *testing.T) {
	nodes := buildNodes(buildTestTree(), "base")

	if len(nodes) != 3 {
		t.Fatalf("根节点数 = %d, 期望 3", len(nodes))
	}

	folderA := nodes[0]
	if !folderA.IsFolder || folderA.Title != "文件夹A" {
		t.Fatalf("nodes[0] = %+v, 期望文件夹 文件夹A", folderA)
	}

	// 文件夹自身 key 也经 spider.TrackKey 计算
	wantFolderKey := spider.TrackKey("", "base", "文件夹A")
	if folderA.Key != wantFolderKey {
		t.Fatalf("文件夹 key = %q, 期望 %q", folderA.Key, wantFolderKey)
	}

	if len(folderA.Children) != 2 {
		t.Fatalf("文件夹A 子节点数 = %d, 期望 2", len(folderA.Children))
	}

	// 排序后子目录（文件夹）在前
	sub := folderA.Children[0]
	if !sub.IsFolder || sub.Title != "子目录" || len(sub.Children) != 2 {
		t.Fatalf("子目录节点 = %+v, 期望含 2 个子文件的文件夹", sub)
	}

	// 文件 key 与 spider.TrackKey 一致：hash 优先
	wav := folderA.Children[1]
	wantWavKey := spider.TrackKey("h1", filepath.Join("base", "文件夹A"), "01.wav")
	if wav.IsFolder || wav.Key != wantWavKey {
		t.Fatalf("01.wav 节点 = %+v, key 期望 %q", wav, wantWavKey)
	}

	// 根级文件自然排序：04.m4a（'0' < 'n'）在 no-hash.txt 之前
	noHash := nodes[2]
	wantNoHashKey := spider.TrackKey("", "base", "no-hash.txt")
	if noHash.IsFolder || noHash.Key != wantNoHashKey {
		t.Fatalf("no-hash.txt key = %q, 期望 %q", noHash.Key, wantNoHashKey)
	}

	// 根级叶子
	leaf := nodes[1]
	if leaf.IsFolder || leaf.Key != spider.TrackKey("h4", "base", "04.m4a") {
		t.Fatalf("04.m4a 节点 = %+v, key 期望 h4", leaf)
	}

	// 树中全部叶子 key 唯一且共 5 个
	keys := leafKeysOf(nodes)
	if len(keys) != 5 {
		t.Fatalf("叶子 key 数 = %d, 期望 5 (%v)", len(keys), keys)
	}
}

func TestToggleNode(t *testing.T) {
	nodes := buildNodes(buildTestTree(), "base")
	folderA := nodes[0]

	tests := []struct {
		name    string
		target  *selectorNode
		initial map[string]bool
		want    map[string]bool
	}{
		{
			name:    "勾叶子-从未选中到选中",
			target:  nodes[1],
			initial: map[string]bool{},
			want:    map[string]bool{"h4": true},
		},
		{
			name:    "再勾叶子-取消选中",
			target:  nodes[1],
			initial: map[string]bool{"h4": true},
			want:    map[string]bool{},
		},
		{
			name:    "勾文件夹-子孙全部置 true",
			target:  folderA,
			initial: map[string]bool{},
			want: map[string]bool{
				spider.TrackKey("h1", filepath.Join("base", "文件夹A"), "01.wav"):         true,
				spider.TrackKey("h2", filepath.Join("base", "文件夹A", "子目录"), "02.mp3"):  true,
				spider.TrackKey("h3", filepath.Join("base", "文件夹A", "子目录"), "03.flac"): true,
			},
		},
		{
			name:    "再勾文件夹-子孙全部置 false",
			target:  folderA,
			initial: leafKeysOf(nodes),
			want: map[string]bool{ // 仅根级叶子保持
				spider.TrackKey("", "base", "no-hash.txt"): true,
				"h4": true,
			},
		},
		{
			name:    "部分选中的文件夹-勾选补全为全选",
			target:  folderA,
			initial: map[string]bool{"h1": true},
			want: map[string]bool{
				"h1": true,
				spider.TrackKey("h2", filepath.Join("base", "文件夹A", "子目录"), "02.mp3"):  true,
				spider.TrackKey("h3", filepath.Join("base", "文件夹A", "子目录"), "03.flac"): true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := make(map[string]bool)
			for k, v := range tt.initial {
				keys[k] = v
			}
			toggleNode(keys, tt.target)

			if len(keys) != len(tt.want) {
				t.Fatalf("选中 key 数 = %d, 期望 %d (%v)", len(keys), len(tt.want), keys)
			}
			for k, want := range tt.want {
				if keys[k] != want {
					t.Fatalf("key %q = %v, 期望 %v", k, keys[k], want)
				}
			}
		})
	}
}

func TestCountNode(t *testing.T) {
	nodes := buildNodes(buildTestTree(), "base")
	folderA := nodes[0]
	sub := folderA.Children[0] // 排序后文件夹在前
	wav := folderA.Children[1]
	leaf := nodes[2]

	tests := []struct {
		name         string
		node         *selectorNode
		selected     map[string]bool
		wantSelected int
		wantTotal    int
	}{
		{
			name:         "嵌套目录部分选中",
			node:         folderA,
			selected:     map[string]bool{"h1": true, "h2": true},
			wantSelected: 2,
			wantTotal:    3,
		},
		{
			name:         "子目录无选中",
			node:         sub,
			selected:     map[string]bool{"h1": true},
			wantSelected: 0,
			wantTotal:    2,
		},
		{
			name:         "叶子已选中",
			node:         wav,
			selected:     map[string]bool{"h1": true},
			wantSelected: 1,
			wantTotal:    1,
		},
		{
			name:         "叶子未选中",
			node:         leaf,
			selected:     map[string]bool{},
			wantSelected: 0,
			wantTotal:    1,
		},
		{
			name:         "根级无 hash 叶子",
			node:         nodes[2],
			selected:     map[string]bool{},
			wantSelected: 0,
			wantTotal:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSelected, gotTotal := countNode(tt.node, tt.selected)
			if gotSelected != tt.wantSelected || gotTotal != tt.wantTotal {
				t.Fatalf("countNode = (%d, %d), 期望 (%d, %d)",
					gotSelected, gotTotal, tt.wantSelected, tt.wantTotal)
			}
		})
	}
}

func TestPageCursor(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		delta  int
		total  int
		want   int
	}{
		{"向下翻页一屏", 0, 10, 25, 10},
		{"向下翻页到底部夹紧", 20, 10, 25, 24},
		{"向上翻页一屏", 15, -10, 25, 5},
		{"向上翻页到顶部夹紧", 5, -10, 25, 0},
		{"空列表返回0", 0, 10, 0, 0},
		{"单行列表保持0", 0, 5, 1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pageCursor(tt.cursor, tt.delta, tt.total)
			if got != tt.want {
				t.Fatalf("pageCursor(%d, %d, %d) = %d, 期望 %d",
					tt.cursor, tt.delta, tt.total, got, tt.want)
			}
		})
	}
}

func TestSyncOffset(t *testing.T) {
	tests := []struct {
		name       string
		cursor     int
		offset     int
		height     int
		total      int
		wantOffset int
	}{
		// height=10 时 listHeight()=7
		{"光标在窗口上方-窗口上移", 2, 10, 10, 30, 2},
		{"光标在窗口下方-窗口下移", 15, 0, 10, 30, 9},
		{"光标已在窗口内-不变", 12, 10, 10, 30, 10},
		{"列表缩短后offset越界-跟随光标", 4, 10, 10, 5, 4},
		{"极矮窗口listHeight为1", 3, 0, 2, 30, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &treeSelectorModel{
				cursor: tt.cursor,
				offset: tt.offset,
				height: tt.height,
				flat:   make([]flatRow, tt.total),
			}
			m.syncOffset()
			if m.offset != tt.wantOffset {
				t.Fatalf("syncOffset 后 offset = %d, 期望 %d", m.offset, tt.wantOffset)
			}
			if m.cursor != tt.cursor {
				t.Fatalf("syncOffset 不应修改 cursor = %d, 期望 %d", m.cursor, tt.cursor)
			}
		})
	}
}

func TestTruncateWidth(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string // wantWidth 非负时仅断言宽度
	}{
		{"短ASCII不截断", "hello.txt", 20, "hello.txt"},
		{"短日文不截断", "日本語タイトル", 14, "日本語タイトル"},
		{"恰好整行-ASCII", "hello world", 11, "hello world"},
		{"恰好整行-日文", "あいうえお", 10, "あいうえお"},
		{"ASCII超宽截断加省略号", "hello world", 8, "hello w…"},
		{"max为0返回空", "hello", 0, ""},
		{"max为1返回省略号", "日本語", 1, "…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateWidth(tt.s, tt.max)
			if got != tt.want {
				t.Fatalf("truncateWidth(%q, %d) = %q, 期望 %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}

func TestTruncateWidthCapped(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
	}{
		{"日文长标题截断", "とある日文のとても長い音声作品タイトルファイル名.wav", 20},
		{"中英混排截断", "中文abcEnglish日本語もっと長いタイトル.mp3", 15},
		{"全角省略号边界", "日本語のタイトルがここに入ります", 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateWidth(tt.s, tt.max)
			w := runewidth.StringWidth(got)
			if w > tt.max {
				t.Fatalf("truncateWidth(%q, %d) = %q, 渲染宽度 %d 超过 %d", tt.s, tt.max, got, w, tt.max)
			}
		})
	}
}

func TestInitialKeys(t *testing.T) {
	nodes := buildNodes(buildTestTree(), "base")
	allLeafKeys := leafKeysOf(nodes)

	tests := []struct {
		name    string
		initial map[string]bool
		want    map[string]bool
	}{
		{
			name:    "nil-全选",
			initial: nil,
			want:    allLeafKeys,
		},
		{
			name:    "给定集合-恰为该集合",
			initial: map[string]bool{"h1": true, "h4": true},
			want:    map[string]bool{"h1": true, "h4": true},
		},
		{
			name:    "空集合-全不选",
			initial: map[string]bool{},
			want:    map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := initSelectedKeys(nodes, tt.initial)
			if len(got) != len(tt.want) {
				t.Fatalf("初始选中 key 数 = %d, 期望 %d (%v)", len(got), len(tt.want), got)
			}
			for k, want := range tt.want {
				if got[k] != want {
					t.Fatalf("key %q = %v, 期望 %v", k, got[k], want)
				}
			}
		})
	}
}

// TestBuildNodesKeysMatchSpiderAnalysis 跨层一致性：ui 侧 buildNodes（带 basePath）生成的 key
// 必须能被 spider 侧 AnalyzeFormats/FilesForKeys 精确命中。
// 覆盖 hash 文件与无 hash 回退（path|title）文件——防止两端回退口径再次分叉
// （本缺陷逃逸的根因即两端测试各自用 TrackKey 却实参不同）。
func TestBuildNodesKeysMatchSpiderAnalysis(t *testing.T) {
	const basePath = "downloads/RJ123456"
	tree := buildTestTree()
	nodes := buildNodes(tree, basePath)
	analysis := spider.AnalyzeFormats(tree, basePath)

	// pickFile 构造仅勾选单个叶子的 key 集合
	pickFile := func(n *selectorNode) map[string]bool {
		return map[string]bool{n.Key: true}
	}

	tests := []struct {
		name       string
		keys       map[string]bool
		wantTitles []string
	}{
		{
			name:       "根级无hash文件-回退key两端一致",
			keys:       pickFile(nodes[2]),
			wantTitles: []string{"no-hash.txt"},
		},
		{
			name:       "目录内hash文件",
			keys:       pickFile(nodes[0].Children[1]),
			wantTitles: []string{"01.wav"},
		},
		{
			name:       "嵌套子目录文件",
			keys:       pickFile(nodes[0].Children[0].Children[1]),
			wantTitles: []string{"03.flac"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := spider.FilesForKeys(analysis, tt.keys)
			if len(files) != len(tt.wantTitles) {
				t.Fatalf("FilesForKeys 命中数 = %d, 期望 %d (%v)", len(files), len(tt.wantTitles), files)
			}
			for i, want := range tt.wantTitles {
				if files[i].Track.Title != want {
					t.Fatalf("命中文件[%d] = %q, 期望 %q", i, files[i].Track.Title, want)
				}
			}
		})
	}

	// 全选：叶子 key 集应命中树上全部 5 个文件
	files := spider.FilesForKeys(analysis, leafKeysOf(nodes))
	if len(files) != 5 {
		t.Fatalf("全选命中数 = %d, 期望 5", len(files))
	}
}

// pressKey 向模型发送按键消息并返回更新后的模型。
func pressKey(t *testing.T, m *treeSelectorModel, key tea.KeyMsg) *treeSelectorModel {
	t.Helper()
	next, _ := m.Update(key)
	nm, ok := next.(*treeSelectorModel)
	if !ok {
		t.Fatal("Update 返回了非 *treeSelectorModel 类型")
	}
	return nm
}

// TestSelectorKeybindings 验证确认/退出/展开/折叠的按键分配：
// enter 确认（无选中时仅警告不退出），q/Esc 退出，→/l 展开，←/h 折叠。
func TestSelectorKeybindings(t *testing.T) {
	newModel := func() *treeSelectorModel {
		// nil initialKeys → 全选，enter 可直接确认
		return newTreeSelectorModel("downloads/RJ123456", buildTestTree(), nil)
	}

	t.Run("enter确认并退出", func(t *testing.T) {
		m := pressKey(t, newModel(), tea.KeyMsg{Type: tea.KeyEnter})
		if !m.confirmed || m.cancelled {
			t.Fatalf("enter 应确认下载, confirmed=%v cancelled=%v", m.confirmed, m.cancelled)
		}
	})

	t.Run("enter无选中时仅警告不退出", func(t *testing.T) {
		m := newTreeSelectorModel("downloads/RJ123456", buildTestTree(), map[string]bool{})
		m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if m.confirmed || m.cancelled {
			t.Fatalf("无选中时 enter 不应退出, confirmed=%v cancelled=%v", m.confirmed, m.cancelled)
		}
		if m.statusMsg == "" {
			t.Fatal("无选中时 enter 应给出警告状态消息")
		}
	})

	t.Run("q退出即取消", func(t *testing.T) {
		m := pressKey(t, newModel(), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		if !m.cancelled || m.confirmed {
			t.Fatalf("q 应取消退出, confirmed=%v cancelled=%v", m.confirmed, m.cancelled)
		}
	})

	t.Run("esc退出即取消", func(t *testing.T) {
		m := pressKey(t, newModel(), tea.KeyMsg{Type: tea.KeyEscape})
		if !m.cancelled || m.confirmed {
			t.Fatalf("esc 应取消退出, confirmed=%v cancelled=%v", m.confirmed, m.cancelled)
		}
	})

	t.Run("方向键与l_h展开折叠", func(t *testing.T) {
		m := newModel()
		collapsedRows := len(m.flat) // 初始 3 行：文件夹A + no-hash.txt + 04.m4a

		m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyRight})
		if len(m.flat) <= collapsedRows {
			t.Fatalf("→ 应展开文件夹, 行数 %d 未增加", len(m.flat))
		}
		m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
		if len(m.flat) != collapsedRows {
			t.Fatalf("h 应折叠文件夹, 行数 %d != %d", len(m.flat), collapsedRows)
		}
		m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
		if len(m.flat) <= collapsedRows {
			t.Fatalf("l 应展开文件夹, 行数 %d 未增加", len(m.flat))
		}
		m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyLeft})
		if len(m.flat) != collapsedRows {
			t.Fatalf("← 应折叠文件夹, 行数 %d != %d", len(m.flat), collapsedRows)
		}
	})
}

// TestViewFolderExpandIndicator 验证文件夹行以 v/> 标识展开/折叠状态。
func TestViewFolderExpandIndicator(t *testing.T) {
	m := newTreeSelectorModel("downloads/RJ123456", buildTestTree(), nil)
	m.width = 80
	m.height = 12

	// 折叠态：应含折叠指示符且不渲染子文件
	view := m.View()
	if !strings.Contains(view, "] > ") {
		t.Fatal("折叠状态的文件夹行应包含 > 指示符")
	}
	if strings.Contains(view, "01.wav") {
		t.Fatal("折叠状态下不应渲染子文件 01.wav")
	}

	// 展开态：应含展开指示符且渲染子文件
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyRight})
	view = m.View()
	if !strings.Contains(view, "] v ") {
		t.Fatal("展开状态的文件夹行应包含 v 指示符")
	}
	if !strings.Contains(view, "01.wav") {
		t.Fatal("展开状态下应渲染子文件 01.wav")
	}

	// 文件行以空格占位指示符列：标题相对各自缩进对齐（文件夹行深度 0，文件行深度 1 多一层缩进）
	folderLine, fileLine := "", ""
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, "文件夹A") {
			folderLine = l
		}
		if strings.Contains(l, "01.wav") {
			fileLine = l
		}
	}
	if folderLine != "> [x] v 文件夹A (3/3)" {
		t.Fatalf("文件夹行渲染不符, got %q", folderLine)
	}
	if fileLine != "    [x]   01.wav" {
		t.Fatalf("文件行渲染不符（应占位指示符列）, got %q", fileLine)
	}
}
