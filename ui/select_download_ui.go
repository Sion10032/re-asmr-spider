package ui

import (
	"strings"

	"re-asmr-spider/app"
	"re-asmr-spider/i18n"
	"re-asmr-spider/spider"
	"re-asmr-spider/utils"
)

// SelectAndDownload 手动文件树选择模式的下载入口：
// 构建 OnSelect 回调（选择器返回 nil 或未确认时一律按取消处理，由编排层中止流程），
// 交给 DownloadWithSelection 编排（沿用记录或打开 TUI 选择，reselect 为 true 时强制重新选择）。
func SelectAndDownload(application *app.App, tasks []string, reselect bool, waitForInput bool) {
	onSelect := func(rjID string, tree []spider.Track, initialKeys map[string]bool) (map[string]bool, bool) {
		// initialKeys 由编排层在重选场景下从旧记录的 key 集构造，直接透传给 TUI 做预勾选
		selectedKeys, confirmed := RunTreeSelector(rjID, tree, initialKeys)
		if selectedKeys == nil {
			// 程序出错路径：按未确认处理，编排层会中止下载
			return nil, false
		}
		return selectedKeys, confirmed
	}

	application.DownloadWithSelection(app.SelectionFlowOptions{
		Tasks:        tasks,
		Reselect:     reselect,
		WaitForInput: waitForInput,
		OnSelect:     onSelect,
	})
}

// StartSelectDownload 手动选择下载界面：读 RJ 号（空格分隔，与 StartDownload 一致），空输入警告后返回菜单。
func StartSelectDownload(application *app.App) {
	utils.Info(i18n.T("select_download_title"))
	utils.Info(i18n.T("select_download_input_hint"))

	rjNumbers := ReadInput(i18n.T("prompt_rj_number"))
	if rjNumbers == "" {
		utils.Warning(i18n.T("no_rj_input"))
		return
	}

	tasks := strings.Split(rjNumbers, " ")
	SelectAndDownload(application, tasks, false, true)
}
