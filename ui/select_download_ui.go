package ui

import (
	"strings"

	"re-asmr-spider/app"
	"re-asmr-spider/i18n"
	"re-asmr-spider/utils"
)

// SelectAndDownload 手动文件树选择模式的下载入口：
// 启动业务会话（登录/监控）→ 预解析阶段（TUI 外逐任务拉取文件树并判定沿用，正常日志反馈）→
// 仅当存在需要手动选择的任务时才打开全屏会话（预解析结果缓存传入，会话内无加载态）→ 执行下载。
// 全部任务都能沿用记录时全程不弹 TUI；多作品选择时单个全屏程序内完成，作品间切换零回显。
// 返回整个流程是否成功，供 CLI 路径决定退出码；菜单路径可忽略返回值。
func SelectAndDownload(application *app.App, tasks []string, reselect bool, waitForInput bool) bool {
	session := application.StartSelectionSession(tasks, reselect)
	if session == nil {
		// 登录失败等启动错误已在会话启动时打印
		return false
	}

	// 预解析：逐任务拉取文件树并判定沿用/需选择。
	// 本阶段在 TUI 外，日志正常输出：拉取前后均有反馈（与普通下载路径同一对日志键），
	// 多作品连续拉取期间终端不静默。
	pending := make([]app.TaskPreparation, 0, len(tasks))
	for _, task := range tasks {
		utils.Info(i18n.T("fetching_work_info", task))
		prep := session.Prepare(task)
		if prep.Err != nil {
			utils.Error(i18n.T("selection_fetch_failed", prep.Task, prep.Err))
			continue
		}
		utils.Success(i18n.T("work_info_fetched", prep.Task))
		if prep.Reused {
			session.Reuse(prep)
			utils.Info(i18n.T("selection_reused", prep.Task, prep.ReusedCount))
			if prep.Missing > 0 {
				utils.Warning(i18n.T("selection_missing_warn", prep.Task, prep.Missing))
			}
			utils.Info(i18n.T("selection_updated_hint"))
			continue
		}
		if prep.RecordErr != nil {
			utils.Warning(i18n.T("selection_record_read_failed", prep.Task, prep.RecordErr))
		}
		if prep.Stale {
			utils.Warning(i18n.T("selection_stale", prep.Task))
		}
		pending = append(pending, prep)
	}

	// 仅存在需要手动选择的任务时才打开全屏会话
	if len(pending) > 0 {
		proceed, notices := RunSelectionSession(session, pending)
		for _, line := range notices {
			utils.Warning(line)
		}
		if !proceed {
			// 用户中止：已保存的选择记录与下载状态保留，便于之后继续
			utils.Info(i18n.T("selection_cancelled"))
			return false
		}
	}

	return session.Finish(waitForInput)
}

// StartSelectDownload 手动选择下载界面：读作品号（空格分隔，与 StartDownload 一致），空输入警告后返回菜单。
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
