package ui

import (
	"path/filepath"
	"testing"

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

	// 文件 key 与 spider.TrackKey 一致：hash 优先
	wav := folderA.Children[0]
	wantWavKey := spider.TrackKey("h1", filepath.Join("base", "文件夹A"), "01.wav")
	if wav.IsFolder || wav.Key != wantWavKey {
		t.Fatalf("01.wav 节点 = %+v, key 期望 %q", wav, wantWavKey)
	}

	// 嵌套子目录层级正确
	sub := folderA.Children[1]
	if !sub.IsFolder || sub.Title != "子目录" || len(sub.Children) != 2 {
		t.Fatalf("子目录节点 = %+v, 期望含 2 个子文件的文件夹", sub)
	}

	// 根级无 hash 文件：key 回退为 path|title
	noHash := nodes[1]
	wantNoHashKey := spider.TrackKey("", "base", "no-hash.txt")
	if noHash.IsFolder || noHash.Key != wantNoHashKey {
		t.Fatalf("no-hash.txt key = %q, 期望 %q", noHash.Key, wantNoHashKey)
	}

	// 根级叶子
	leaf := nodes[2]
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
	nodes := buildNodes(buildTestTree(), "")
	folderA := nodes[0]

	tests := []struct {
		name    string
		target  *selectorNode
		initial map[string]bool
		want    map[string]bool
	}{
		{
			name:    "勾叶子-从未选中到选中",
			target:  nodes[2],
			initial: map[string]bool{},
			want:    map[string]bool{"h4": true},
		},
		{
			name:    "再勾叶子-取消选中",
			target:  nodes[2],
			initial: map[string]bool{"h4": true},
			want:    map[string]bool{},
		},
		{
			name:    "勾文件夹-子孙全部置 true",
			target:  folderA,
			initial: map[string]bool{},
			want: map[string]bool{
				spider.TrackKey("h1", filepath.Join("", "文件夹A"), "01.wav"):         true,
				spider.TrackKey("h2", filepath.Join("", "文件夹A", "子目录"), "02.mp3"):  true,
				spider.TrackKey("h3", filepath.Join("", "文件夹A", "子目录"), "03.flac"): true,
			},
		},
		{
			name:    "再勾文件夹-子孙全部置 false",
			target:  folderA,
			initial: leafKeysOf(nodes),
			want: map[string]bool{ // 仅根级叶子保持
				spider.TrackKey("", "", "no-hash.txt"): true,
				"h4":                                   true,
			},
		},
		{
			name:    "部分选中的文件夹-勾选补全为全选",
			target:  folderA,
			initial: map[string]bool{"h1": true},
			want: map[string]bool{
				"h1": true,
				spider.TrackKey("h2", filepath.Join("", "文件夹A", "子目录"), "02.mp3"):  true,
				spider.TrackKey("h3", filepath.Join("", "文件夹A", "子目录"), "03.flac"): true,
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
	nodes := buildNodes(buildTestTree(), "")
	folderA := nodes[0]
	sub := folderA.Children[1]
	wav := folderA.Children[0]
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
			node:         nodes[1],
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
		// height=10 时 listHeight()=6
		{"光标在窗口上方-窗口上移", 2, 10, 10, 30, 2},
		{"光标在窗口下方-窗口下移", 15, 0, 10, 30, 10},
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
	nodes := buildNodes(buildTestTree(), "")
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
