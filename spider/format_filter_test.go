package spider

import (
	"sort"
	"testing"
)

// titlesOf 收集过滤结果中的文件名，排序后便于断言
func titlesOf(files []*FileInfo) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Track.Title)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseFormatList(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"single without dot", "wav", []string{".wav"}},
		{"already dotted", ".flac", []string{".flac"}},
		{"multiple mixed case and spaces", "WAV, .Flac , mp3", []string{".wav", ".flac", ".mp3"}},
		{"empty string", "", []string{}},
		{"only commas and spaces", " , ,", []string{}},
		{"trailing comma", "wav,", []string{".wav"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseFormatList(tc.input)
			if !equalStrings(got, tc.want) {
				t.Fatalf("ParseFormatList(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// 复现反馈场景：wav 与 mp3 文件名不同（不构成同名冲突组），
// -only-formats wav 仍应只保留 wav，丢弃 mp3 及封面/字幕。
func TestApplyFilter_OnlyFormats_DropsNonWhitelistedWithoutConflict(t *testing.T) {
	tracks := []track{
		{Type: "folder", Title: "wav", Children: []track{
			{Type: "audio", Title: "track01.wav"},
			{Type: "audio", Title: "track02.wav"},
		}},
		{Type: "folder", Title: "mp3", Children: []track{
			{Type: "audio", Title: "song_a.mp3"},
			{Type: "audio", Title: "song_b.mp3"},
		}},
		{Type: "image", Title: "cover.jpg"},
		{Type: "text", Title: "subtitle.lrc"},
	}

	analysis := AnalyzeFormats(tracks, "base")
	result := analysis.ApplyFilter(&FilterStrategy{
		Mode:        "priority",
		OnlyFormats: []string{".wav"},
	})

	got := titlesOf(result)
	want := []string{"track01.wav", "track02.wav"}
	if !equalStrings(got, want) {
		t.Fatalf("only-formats wav: got %v, want %v", got, want)
	}
}

// -only-formats wav 配合 -include-formats jpg,lrc：
// 保留 wav，并把封面/字幕加回，但 mp3 仍被丢弃。
func TestApplyFilter_OnlyFormats_WithIncludeFormats(t *testing.T) {
	tracks := []track{
		{Type: "audio", Title: "track01.wav"},
		{Type: "audio", Title: "song_a.mp3"},
		{Type: "image", Title: "cover.jpg"},
		{Type: "text", Title: "subtitle.lrc"},
	}

	analysis := AnalyzeFormats(tracks, "base")
	result := analysis.ApplyFilter(&FilterStrategy{
		Mode:           "priority",
		OnlyFormats:    []string{".wav"},
		IncludeFormats: []string{".jpg", ".lrc"},
	})

	got := titlesOf(result)
	want := []string{"cover.jpg", "subtitle.lrc", "track01.wav"}
	if !equalStrings(got, want) {
		t.Fatalf("only-formats+include: got %v, want %v", got, want)
	}
}

// -only-formats wav,flac 配合 -format-priority flac：
// 同名 track01 的三种格式中，mp3 被白名单排除，wav 输给 flac，只剩 flac；
// 另有一个无同名对的 bonus.mp3，必须被白名单整体丢弃（否则优先级逻辑会放它过去）。
func TestApplyFilter_OnlyFormats_PriorityResolvesWithinWhitelist(t *testing.T) {
	tracks := []track{
		{Type: "audio", Title: "track01.wav"},
		{Type: "audio", Title: "track01.flac"},
		{Type: "audio", Title: "track01.mp3"},
		{Type: "audio", Title: "bonus.mp3"},
	}

	analysis := AnalyzeFormats(tracks, "base")
	result := analysis.ApplyFilter(&FilterStrategy{
		Mode:            "priority",
		OnlyFormats:     []string{".wav", ".flac"},
		PriorityFormats: []string{".flac"},
	})

	got := titlesOf(result)
	want := []string{"track01.flac"}
	if !equalStrings(got, want) {
		t.Fatalf("only-formats+priority: got %v, want %v", got, want)
	}
}

// -only-formats wav,flac 但未给 -format-priority：白名单内同名 wav/flac 无优先级裁决，
// 应两者都保留（"未指定优先级=保留所有白名单格式"），而 mp3 仍被白名单丢弃。
func TestApplyFilter_OnlyFormats_NoPriorityKeepsAllWhitelisted(t *testing.T) {
	tracks := []track{
		{Type: "audio", Title: "track01.wav"},
		{Type: "audio", Title: "track01.flac"},
		{Type: "audio", Title: "track01.mp3"},
	}

	analysis := AnalyzeFormats(tracks, "base")
	result := analysis.ApplyFilter(&FilterStrategy{
		Mode:        "priority",
		OnlyFormats: []string{".wav", ".flac"},
	})

	got := titlesOf(result)
	want := []string{"track01.flac", "track01.wav"}
	if !equalStrings(got, want) {
		t.Fatalf("only-formats no-priority: got %v, want %v", got, want)
	}
}

// 回归保护：未设置 OnlyFormats 时，无冲突场景应保持原行为（全部保留）。
func TestApplyFilter_NoOnlyFormats_KeepsAllWhenNoConflict(t *testing.T) {
	tracks := []track{
		{Type: "audio", Title: "track01.wav"},
		{Type: "audio", Title: "song_a.mp3"},
		{Type: "image", Title: "cover.jpg"},
	}

	analysis := AnalyzeFormats(tracks, "base")
	result := analysis.ApplyFilter(&FilterStrategy{Mode: "priority"})

	got := titlesOf(result)
	want := []string{"cover.jpg", "song_a.mp3", "track01.wav"}
	if !equalStrings(got, want) {
		t.Fatalf("no only-formats: got %v, want %v", got, want)
	}
}
