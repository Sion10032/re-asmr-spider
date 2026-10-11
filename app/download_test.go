package app

import (
	"testing"

	"re-asmr-spider/spider"
)

// TestFilterForTask 验证每任务过滤策略的选取逻辑：
// TaskFilters 命中优先，否则回落到全局 FormatFilter。
func TestFilterForTask(t *testing.T) {
	globalFilter := &spider.FilterStrategy{Mode: "all"}
	taskFilter := &spider.FilterStrategy{Mode: "priority", PriorityFormats: []string{"mp3"}}

	tests := []struct {
		name   string
		opts   DownloadOptions
		task   string
		expect *spider.FilterStrategy
	}{
		{
			name:   "TaskFilters为nil时回落FormatFilter",
			opts:   DownloadOptions{FormatFilter: globalFilter},
			task:   "RJ123456",
			expect: globalFilter,
		},
		{
			name: "命中TaskFilters策略",
			opts: DownloadOptions{
				FormatFilter: globalFilter,
				TaskFilters:  map[string]*spider.FilterStrategy{"123456": taskFilter},
			},
			task:   "RJ123456",
			expect: taskFilter,
		},
		{
			name: "TaskFilters无此任务时回落FormatFilter",
			opts: DownloadOptions{
				FormatFilter: globalFilter,
				TaskFilters:  map[string]*spider.FilterStrategy{"654321": taskFilter},
			},
			task:   "RJ123456",
			expect: globalFilter,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterForTask(tc.opts, tc.task)
			if got != tc.expect {
				t.Fatalf("filterForTask(%q) 返回 %v, 期望 %v", tc.task, got, tc.expect)
			}
		})
	}
}
