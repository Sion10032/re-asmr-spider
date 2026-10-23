package spider

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// makeTestFile 构造测试用 FileInfo；hash 为空时 TrackKey 会回退到 path|title。
func makeTestFile(hash, path, title string) *FileInfo {
	return &FileInfo{
		Track: &Track{Hash: hash, Title: title},
		Path:  path,
	}
}

func TestSaveLoadSelectionRoundtrip(t *testing.T) {
	base := t.TempDir()
	files := []*FileInfo{
		makeTestFile("hash-a", "folder1", "track a.mp3"),
		makeTestFile("", "folder2", "track b.wav"), // hash 为空，走回退 key
	}

	if err := SaveSelection(base, "123456", files); err != nil {
		t.Fatalf("SaveSelection() error = %v", err)
	}

	// 记录文件应落在 basePath 下
	if _, err := os.Stat(SelectionPath(base)); err != nil {
		t.Fatalf("selection file not created: %v", err)
	}

	rec, exists, err := LoadSelection(base)
	if err != nil {
		t.Fatalf("LoadSelection() error = %v", err)
	}
	if !exists {
		t.Fatal("LoadSelection() exists = false, want true")
	}
	if rec.RJ != "123456" {
		t.Errorf("RJ = %q, want %q", rec.RJ, "123456")
	}
	if rec.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero, want non-zero UTC time")
	}
	if len(rec.Files) != len(files) {
		t.Fatalf("len(Files) = %d, want %d", len(rec.Files), len(files))
	}
	for i, want := range files {
		got := rec.Files[i]
		wantKey := TrackKey(want.Track.Hash, want.Path, want.Track.Title)
		if got.Key != wantKey {
			t.Errorf("Files[%d].Key = %q, want %q", i, got.Key, wantKey)
		}
		if got.Path != want.Path {
			t.Errorf("Files[%d].Path = %q, want %q", i, got.Path, want.Path)
		}
		if got.Title != want.Track.Title {
			t.Errorf("Files[%d].Title = %q, want %q", i, got.Title, want.Track.Title)
		}
	}
}

func TestLoadSelection_Missing(t *testing.T) {
	rec, exists, err := LoadSelection(t.TempDir())
	if err != nil {
		t.Fatalf("LoadSelection() error = %v, want nil", err)
	}
	if exists {
		t.Error("LoadSelection() exists = true, want false")
	}
	if rec != nil {
		t.Errorf("LoadSelection() rec = %+v, want nil", rec)
	}
}

func TestLoadSelection_Corrupt(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(SelectionPath(base), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	rec, exists, err := LoadSelection(base)
	if err == nil {
		t.Fatal("LoadSelection() error = nil, want error for corrupt JSON")
	}
	if exists {
		t.Error("LoadSelection() exists = true, want false")
	}
	if rec != nil {
		t.Errorf("LoadSelection() rec = %+v, want nil", rec)
	}
}

func TestMatchSelection(t *testing.T) {
	fileA := makeTestFile("hash-a", "f1", "a.mp3")
	fileB := makeTestFile("hash-b", "f1", "b.wav")
	fileC := makeTestFile("", "f2", "c.mp3")
	analysis := &FormatAnalysis{AllFiles: []*FileInfo{fileA, fileB, fileC}}

	keyA := TrackKey("hash-a", "f1", "a.mp3")
	keyB := TrackKey("hash-b", "f1", "b.wav")
	keyC := TrackKey("", "f2", "c.mp3")

	cases := []struct {
		name        string
		recKeys     []string
		wantKeys    []string // 期望命中文件的 key，按 AllFiles 顺序
		wantMissing int
	}{
		{
			name:        "全部命中",
			recKeys:     []string{keyA, keyB, keyC},
			wantKeys:    []string{keyA, keyB, keyC},
			wantMissing: 0,
		},
		{
			name:        "部分命中",
			recKeys:     []string{keyA, keyC, "gone-hash"},
			wantKeys:    []string{keyA, keyC},
			wantMissing: 1,
		},
		{
			name:        "全部失效",
			recKeys:     []string{"gone-1", "gone-2", "gone-3"},
			wantKeys:    nil,
			wantMissing: 3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &SelectionRecord{}
			for _, k := range tc.recKeys {
				rec.Files = append(rec.Files, SelectedFile{Key: k})
			}

			matched, missing := MatchSelection(rec, analysis)

			if missing != tc.wantMissing {
				t.Errorf("missing = %d, want %d", missing, tc.wantMissing)
			}
			if len(matched) != len(tc.wantKeys) {
				t.Fatalf("len(matched) = %d, want %d", len(matched), len(tc.wantKeys))
			}
			for i, f := range matched {
				if got := TrackKey(f.Track.Hash, f.Path, f.Track.Title); got != tc.wantKeys[i] {
					t.Errorf("matched[%d] key = %q, want %q", i, got, tc.wantKeys[i])
				}
			}
		})
	}
}

func TestNewFileManualStrategy(t *testing.T) {
	keys := []string{"hash-a", "hash-b", "hash-a"} // 含重复项
	s := NewFileManualStrategy(keys)

	if s.Mode != ModeFileManual {
		t.Errorf("Mode = %q, want %q", s.Mode, ModeFileManual)
	}
	if len(s.SelectedHashes) != 2 {
		t.Errorf("len(SelectedHashes) = %d, want 2 (去重)", len(s.SelectedHashes))
	}
	for _, k := range []string{"hash-a", "hash-b"} {
		if !s.SelectedHashes[k] {
			t.Errorf("SelectedHashes[%q] = false, want true", k)
		}
	}
}

func TestFilesForKeys(t *testing.T) {
	fileA := makeTestFile("hash-a", "f1", "a.mp3")
	fileB := makeTestFile("hash-b", "f1", "b.wav")
	fileC := makeTestFile("", "f2", "c.mp3") // 回退 key
	analysis := &FormatAnalysis{AllFiles: []*FileInfo{fileA, fileB, fileC}}

	keys := map[string]bool{
		"hash-a":                    true,
		TrackKey("", "f2", "c.mp3"): true, // 回退 key 命中
		"hash-b":                    true,
	}

	got := FilesForKeys(analysis, keys)

	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	wantOrder := []*FileInfo{fileA, fileB, fileC}
	for i, f := range got {
		if f != wantOrder[i] {
			t.Errorf("got[%d] = %+v, want %+v（应保持 AllFiles 顺序）", i, f, wantOrder[i])
		}
	}
}

// 编译期确认 JSON 标签符合约定，防止误改结构体标签。
func TestSelectionJSONTags(t *testing.T) {
	data, err := json.Marshal(SelectionRecord{
		RJ:        "123456",
		UpdatedAt: time.Now().UTC(),
		Files:     []SelectedFile{{Key: "k", Path: "p", Title: "t"}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{"rj", "updated_at", "files"} {
		if _, ok := m[field]; !ok {
			t.Errorf("JSON 缺少字段 %q，实际: %s", field, data)
		}
	}
	if len(m["files"].([]any)) > 0 {
		f := m["files"].([]any)[0].(map[string]any)
		for _, field := range []string{"key", "path", "title"} {
			if _, ok := f[field]; !ok {
				t.Errorf("SelectedFile JSON 缺少字段 %q", field)
			}
		}
	}
}
