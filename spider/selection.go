package spider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SelectionFileName 选择记录文件名，存放在作品下载目录（basePath）下。
const SelectionFileName = ".selection.json"

// SelectedFile 选择记录中的单个文件条目。
type SelectedFile struct {
	Key   string `json:"key"`   // 文件唯一标识，见 TrackKey
	Path  string `json:"path"`  // 所属目录路径
	Title string `json:"title"` // 文件名
}

// SelectionRecord 某个作品的文件选择记录，用于断点续选与重定向后恢复选择。
type SelectionRecord struct {
	RJ        string         `json:"rj"`         // 作品 ID（归一化后的纯数字）
	UpdatedAt time.Time      `json:"updated_at"` // 最近保存时间（UTC）
	Files     []SelectedFile `json:"files"`      // 选中的文件列表
}

// SelectionPath 返回 basePath 目录下选择记录文件的完整路径。
func SelectionPath(basePath string) string {
	return filepath.Join(basePath, SelectionFileName)
}

// SaveSelection 将选中的文件保存为 basePath 下的选择记录；basePath 不存在时会自动创建。
func SaveSelection(basePath, rjID string, files []*FileInfo) error {
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return fmt.Errorf("创建目录 %s 失败: %w", basePath, err)
	}

	rec := SelectionRecord{
		RJ:        rjID,
		UpdatedAt: time.Now().UTC(),
		Files:     make([]SelectedFile, 0, len(files)),
	}
	for _, fi := range files {
		rec.Files = append(rec.Files, SelectedFile{
			Key:   TrackKey(fi.Track.Hash, fi.Path, fi.Track.Title),
			Path:  fi.Path,
			Title: fi.Track.Title,
		})
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化选择记录失败: %w", err)
	}
	if err := os.WriteFile(SelectionPath(basePath), data, 0o644); err != nil {
		return fmt.Errorf("写入选择记录失败: %w", err)
	}
	return nil
}

// LoadSelection 读取 basePath 下的选择记录。
// 文件不存在时返回 (nil, false, nil)；文件存在但内容非法时返回 (nil, false, err)。
func LoadSelection(basePath string) (*SelectionRecord, bool, error) {
	path := SelectionPath(basePath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("读取选择记录失败: %w", err)
	}

	var rec SelectionRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, false, fmt.Errorf("解析选择记录失败: %w", err)
	}
	return &rec, true, nil
}

// MatchSelection 将选择记录与当前文件树匹配：
// matched 为命中的文件（保持 analysis.AllFiles 顺序），
// missing 为记录中存在但在当前树里对不上的条数（树上有但记录没有的文件不计入）。
func MatchSelection(rec *SelectionRecord, analysis *FormatAnalysis) ([]*FileInfo, int) {
	treeKeys := make(map[string]*FileInfo)
	for _, fi := range analysis.AllFiles {
		treeKeys[TrackKey(fi.Track.Hash, fi.Path, fi.Track.Title)] = fi
	}

	matched := make([]*FileInfo, 0, len(rec.Files))
	missing := 0
	for _, sf := range rec.Files {
		if fi, ok := treeKeys[sf.Key]; ok {
			matched = append(matched, fi)
		} else {
			missing++
		}
	}
	return matched, missing
}

// NewFileManualStrategy 构造按选中 key 集合精确筛选的过滤策略；keys 去重成 set。
func NewFileManualStrategy(keys []string) *FilterStrategy {
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	return &FilterStrategy{
		Mode:           ModeFileManual,
		SelectedHashes: set,
	}
}

// FilesForKeys 按 key 集合取出选中的文件，结果保持 analysis.AllFiles 顺序。
func FilesForKeys(analysis *FormatAnalysis, keys map[string]bool) []*FileInfo {
	result := make([]*FileInfo, 0, len(keys))
	for _, fi := range analysis.AllFiles {
		if keys[TrackKey(fi.Track.Hash, fi.Path, fi.Track.Title)] {
			result = append(result, fi)
		}
	}
	return result
}
