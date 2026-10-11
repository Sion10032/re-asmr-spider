package app

import (
	"bufio"
	"os"
	"strings"
	"time"

	"re-asmr-spider/config"
	"re-asmr-spider/i18n"
	"re-asmr-spider/spider"
	"re-asmr-spider/utils"
)

// DownloadOptions 下载选项
type DownloadOptions struct {
	Tasks            []string                                                                          // 作品号列表（RJ/VJ 等，按输入原样使用）
	ShowProgress     bool                                                                              // 是否显示进度信息
	WaitForInput     bool                                                                              // 完成后是否等待用户输入
	OnTimeout        func()                                                                            // 超时回调
	TimeoutDetected  chan bool                                                                         // 超时检测通道
	FormatFilter     *spider.FilterStrategy                                                            // 格式过滤策略
	TaskFilters      map[string]*spider.FilterStrategy                                                 // 每任务过滤策略（key 为任务输入原串），命中优先于 FormatFilter
	OnFormatConflict func(*spider.FormatAnalysis, func(*spider.FilterStrategy)) *spider.FilterStrategy // 格式冲突回调（带保存回调）
}

// filterForTask 选取指定任务的过滤策略：TaskFilters 按任务输入原串精确命中优先，
// 否则回落到全局 FormatFilter（可能为 nil，原样返回）。
func filterForTask(opts DownloadOptions, task string) *spider.FilterStrategy {
	if opts.TaskFilters != nil {
		if filter, ok := opts.TaskFilters[task]; ok {
			return filter
		}
	}
	return opts.FormatFilter
}

// DownloadResult 下载结果
type DownloadResult struct {
	Success     bool
	FailedTasks []spider.FailedTask
	TimedOut    bool
}

// Download 执行下载任务 (CLI和UI共用的核心逻辑)
func (a *App) Download(opts DownloadOptions) *DownloadResult {
	result := &DownloadResult{
		Success: true,
	}

	// 使用配置文件中的设置
	c := spider.NewASMRClient(
		a.Config.MaxTask,
		a.Config.MaxThread,
		a.Config.MaxRetry,
		a.Config.GetBufferSize(),
	)
	c.WorkerPool.Start()

	if opts.ShowProgress {
		utils.Info(i18n.T("using_threads", a.Config.MaxThread, a.Config.MaxTask, a.Config.MaxRetry))
	}

	// 登录
	err := c.Login()
	if err != nil {
		utils.Error(i18n.T("login_failed", err))
		if opts.WaitForInput {
			utils.Warning(i18n.T("press_enter_to_return"))
			reader := bufio.NewReader(os.Stdin)
			reader.ReadString('\n')
		}
		result.Success = false
		result.TimedOut = false
		return result
	}

	// 转换回调函数类型（将带保存回调的格式转换为spider需要的简单格式）
	var convertedCallback func(*spider.FormatAnalysis) *spider.FilterStrategy
	if opts.OnFormatConflict != nil {
		convertedCallback = func(analysis *spider.FormatAnalysis) *spider.FilterStrategy {
			// Download函数不负责保存策略，传递空回调
			return opts.OnFormatConflict(analysis, func(*spider.FilterStrategy) {})
		}
	}

	// 提交下载任务
	for _, task := range opts.Tasks {
		c.DownloadWithFilter(task, filterForTask(opts, task), convertedCallback)
	}

	// 等待任务完成
	done := make(chan bool)
	go func() {
		c.WorkerPool.Wait()

		// 重试失败的任务
		for c.RetryFailedTasks() {
			c.WorkerPool.Wait()
		}
		done <- true
	}()

	// 等待完成或超时
	if opts.TimeoutDetected != nil {
		select {
		case <-done:
			// 正常完成
			result.FailedTasks = c.FailedTasks
			result.Success = true
			result.TimedOut = false
		case <-opts.TimeoutDetected:
			// 检测到超时
			result.Success = false
			result.TimedOut = true
		}
	} else {
		// 无超时检测,直接等待完成
		<-done
		result.FailedTasks = c.FailedTasks
		result.Success = true
		result.TimedOut = false
	}

	// 显示结果
	if result.Success && opts.ShowProgress {
		if len(result.FailedTasks) > 0 {
			utils.Error(i18n.T("download_failed_count", len(result.FailedTasks)))
			utils.Info(i18n.T("failed_files_list"))
			for _, task := range result.FailedTasks {
				utils.Errorf("  - %s", task.FileName)
			}
		} else {
			utils.Success(i18n.T("download_complete"))
		}
	}

	return result
}

// startTimeoutWatcher 启动下载无活动超时监控 goroutine。
// 返回 stop 通道（close 即停止监控）与 timeout 通道（检测到超时时收到通知，带缓冲）。
// 供 DownloadWithMonitorAndFormatSelector 与 DownloadWithSelection 共用。
func startTimeoutWatcher() (stop chan bool, timeout chan bool) {
	stop = make(chan bool)
	timeout = make(chan bool, 1)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if utils.GlobalMonitor.IsTimeout() {
					inactiveTime := utils.GlobalMonitor.GetInactiveTime()
					utils.Warning(i18n.T("timeout_detected", inactiveTime.Round(time.Second)))
					timeout <- true
					return
				}
			case <-stop:
				return
			}
		}
	}()

	return stop, timeout
}

// 带超时监控和格式选择的下载。
// baseFilter 为预先确定的全局过滤策略（如交互模式选择的 -only-formats 硬白名单），
// 为 nil 时退回到原有的"按冲突逐个询问"流程，行为不变。
func (a *App) DownloadWithMonitorAndFormatSelector(tasks []string, baseFilter *spider.FilterStrategy, onFormatConflict func(*spider.FormatAnalysis, func(*spider.FilterStrategy)) *spider.FilterStrategy) {
	// 先暂时保存下载状态（不含格式策略）
	if err := config.SaveDownloadState(a.Config, tasks); err != nil {
		utils.Error(i18n.T("save_download_state_failed", err))
	}

	// 启动活动监控
	utils.GlobalMonitor.Start()
	defer utils.GlobalMonitor.Stop()

	// 创建保存回调函数，供格式选择器在每次用户选择后立即保存
	saveStrategyCallback := func(strategy *spider.FilterStrategy) {
		if strategy != nil {
			configStrategy := convertToConfigStrategy(strategy)
			if err := config.SaveDownloadStateWithStrategy(a.Config, tasks, configStrategy); err != nil {
				utils.Error(i18n.T("save_download_state_failed", err))
			}
		}
	}

	// 是否使用全局硬白名单：此时跳过逐个冲突询问，直接整体过滤。
	useGlobalFilter := baseFilter != nil && len(baseFilter.OnlyFormats) > 0
	if useGlobalFilter {
		// 预先持久化策略，确保中断后续传能恢复同样的过滤。
		saveStrategyCallback(baseFilter)
	}

	// 创建一个包装的回调函数，注入保存回调
	wrappedCallback := func(analysis *spider.FormatAnalysis, _ func(*spider.FilterStrategy)) *spider.FilterStrategy {
		if onFormatConflict != nil {
			// 调用格式选择器，传入保存回调
			return onFormatConflict(analysis, saveStrategyCallback)
		}
		return nil
	}

	// 使用循环而不是递归，避免栈溢出
	for {
		// 启动超时检测goroutine
		stopMonitor, timeoutDetected := startTimeoutWatcher()

		// 执行下载
		opts := DownloadOptions{
			Tasks:           tasks,
			ShowProgress:    true,
			WaitForInput:    false,
			TimeoutDetected: timeoutDetected,
		}
		if useGlobalFilter {
			// 全局硬白名单：作为预过滤策略整体应用，不再逐个询问冲突。
			opts.FormatFilter = baseFilter
		} else {
			opts.OnFormatConflict = wrappedCallback
		}
		result := a.Download(opts)

		// 停止监控
		close(stopMonitor)

		// 如果检测到超时，重启下载（循环继续）
		if result.TimedOut {
			utils.Warning(i18n.T("download_interrupted"))
			time.Sleep(2 * time.Second)
			continue // 继续循环重试
		}

		// 下载成功，跳出循环
		break
	}

	// 清除下载状态
	if err := config.ClearDownloadState(a.Config); err != nil {
		utils.Error(i18n.T("clear_download_state_failed", err))
	}

	// 等待用户输入
	utils.Info(i18n.T("press_enter_to_return"))
	reader := bufio.NewReader(os.Stdin)
	reader.ReadString('\n')
}

// SelectionFlowOptions 手动文件树选择模式的下载编排选项
type SelectionFlowOptions struct {
	Tasks        []string // 作品号列表（RJ/VJ 等，按输入原样使用）
	Reselect     bool     // 为 true 时忽略已有选择记录，强制重新选择
	WaitForInput bool     // 完成后是否等待用户输入
	// OnSelect 需要用户选择文件时回调：rjID 为归一化后的纯数字 ID，basePath 为作品下载根目录
	// （"downloads/"+任务输入原串，与 spider 侧文件路径前缀一致，用于回退 key 计算），tree 为完整文件树，
	// initialKeys 为重选时预勾选的 key 集（首次选择为 nil）；返回选中的 key 集与是否确认。
	// 为 nil 时需要选择的任务将被跳过，仅沿用已有记录下载。
	OnSelect func(rjID string, basePath string, tree []spider.Track, initialKeys map[string]bool) (selectedKeys map[string]bool, confirmed bool)
}

// DownloadWithSelection 手动文件树选择模式的下载编排：
// 保存选择模式状态 → 登录 → 逐任务沿用选择记录或回调选择并落盘 → 按每任务策略超时重试循环下载。
// 返回整个流程是否成功：登录失败、用户中止或全部任务被跳过时为 false；
// 下载完成后与现有 CLI 下载路径口径一致（无失败文件视为成功）。
func (a *App) DownloadWithSelection(opts SelectionFlowOptions) bool {
	// 保存手动选择模式的下载状态（含 SelectionMode 标记）
	if err := config.SaveDownloadStateForSelection(a.Config, opts.Tasks); err != nil {
		utils.Error(i18n.T("save_download_state_failed", err))
	}

	// 创建客户端并登录
	c := spider.NewASMRClient(
		a.Config.MaxTask,
		a.Config.MaxThread,
		a.Config.MaxRetry,
		a.Config.GetBufferSize(),
	)
	c.WorkerPool.Start()

	if err := c.Login(); err != nil {
		utils.Error(i18n.T("login_failed", err))
		if err := config.ClearDownloadState(a.Config); err != nil {
			utils.Error(i18n.T("clear_download_state_failed", err))
		}
		return false
	}

	// 启动活动监控
	utils.GlobalMonitor.Start()
	defer utils.GlobalMonitor.Stop()

	// 逐任务解析选择：沿用记录或回调选择，收齐每任务的过滤策略。
	// 仅成功建立策略的任务才会进入下载循环，避免被跳过的任务回落全局策略后全量下载。
	strategies := make(map[string]*spider.FilterStrategy)
	selectedTasks := make([]string, 0, len(opts.Tasks))
	for _, task := range opts.Tasks {
		id := task
		basePath := "downloads/" + id

		tracks, err := c.GetVoiceTracks(id)
		if err != nil {
			utils.Error(i18n.T("selection_fetch_failed", id, err))
			continue
		}
		analysis := spider.AnalyzeFormats(tracks, basePath)

		var keys []string
		needSelect := true

		// 读取选择记录；记录损坏时按无可用记录处理（警告后走选择路径）
		rec, exists, err := spider.LoadSelection(basePath)
		if err != nil {
			utils.Warning(i18n.T("selection_record_read_failed", id, err))
		}

		// 有记录且未强制重选：尝试沿用
		if exists && !opts.Reselect {
			matched, missing := spider.MatchSelection(rec, analysis)
			if len(matched) > 0 {
				utils.Info(i18n.T("selection_reused", id, len(matched)))
				utils.Info(i18n.T("selection_updated_hint"))
				if missing > 0 {
					utils.Warning(i18n.T("selection_missing_warn", id, missing))
				}
				for _, fi := range matched {
					keys = append(keys, spider.TrackKey(fi.Track.Hash, fi.Path, fi.Track.Title))
				}
				needSelect = false
			} else {
				utils.Warning(i18n.T("selection_stale", id))
			}
		}

		if needSelect {
			if opts.OnSelect == nil {
				utils.Warning(i18n.T("selection_no_selector", id))
				continue
			}

			// 重选时把上次记录的 key 集作为预勾选传入
			var initialKeys map[string]bool
			if opts.Reselect && exists && rec != nil {
				initialKeys = make(map[string]bool, len(rec.Files))
				for _, sf := range rec.Files {
					initialKeys[sf.Key] = true
				}
			}

			selectedKeys, confirmed := opts.OnSelect(id, basePath, tracks, initialKeys)
			if !confirmed {
				// 用户取消：中止整个流程；已保存的选择记录与下载状态保留，便于之后继续
				utils.Info(i18n.T("selection_cancelled"))
				return false
			}
			if len(selectedKeys) == 0 {
				utils.Warning(i18n.T("selection_task_skipped", id))
				continue
			}

			// 立即落盘选择记录，防后续流程中断丢选择
			files := spider.FilesForKeys(analysis, selectedKeys)
			if err := spider.SaveSelection(basePath, id, files); err != nil {
				utils.Error(i18n.T("selection_save_failed", id, err))
			}
			for k := range selectedKeys {
				keys = append(keys, k)
			}
		}

		strategies[id] = spider.NewFileManualStrategy(keys)
		selectedTasks = append(selectedTasks, task)
	}

	// 与现有 CLI 下载路径口径一致：无失败文件视为成功；全部任务被跳过时未发生下载，视为失败
	success := false

	// 超时重试循环：与 DownloadWithMonitorAndFormatSelector 同构；
	// 全部任务被跳过（获取文件树失败/无选择器/未选中文件）时不进入下载循环，直接走收尾
	if len(selectedTasks) > 0 {
		var result *DownloadResult
		for {
			// 启动超时检测goroutine
			stopMonitor, timeoutDetected := startTimeoutWatcher()

			// 执行下载（仅含成功建立策略的任务）
			result = a.Download(DownloadOptions{
				Tasks:           selectedTasks,
				ShowProgress:    true,
				WaitForInput:    false,
				TimeoutDetected: timeoutDetected,
				TaskFilters:     strategies,
			})

			// 停止监控
			close(stopMonitor)

			// 如果检测到超时，重启下载（循环继续）
			if result.TimedOut {
				utils.Warning(i18n.T("download_interrupted"))
				time.Sleep(2 * time.Second)
				continue
			}

			// 下载完成，跳出循环
			break
		}

		success = result.Success && len(result.FailedTasks) == 0
	}

	// 清除下载状态
	if err := config.ClearDownloadState(a.Config); err != nil {
		utils.Error(i18n.T("clear_download_state_failed", err))
	}

	// 等待用户输入
	if opts.WaitForInput {
		utils.Info(i18n.T("press_enter_to_return"))
		reader := bufio.NewReader(os.Stdin)
		reader.ReadString('\n')
	}

	return success
}

// 检查并处理未完成的下载
func (a *App) CheckUnfinishedDownload(onContinue func(tasks []string, savedStrategy *config.FormatStrategy, selectionMode bool)) {
	if a.Config.DownloadState.InProgress && len(a.Config.DownloadState.Tasks) > 0 {
		utils.Warning(i18n.T("unfinished_download_detected"))
		utils.Info(i18n.T("unfinished_tasks", strings.Join(a.Config.DownloadState.Tasks, ", ")))

		// 显示已保存的格式策略信息
		if a.Config.DownloadState.FormatStrategy != nil {
			utils.Info(i18n.T("format_strategy_saved"))
		}

		reader := bufio.NewReader(os.Stdin)
		utils.Info(i18n.T("continue_download_prompt"))
		input, _ := reader.ReadString('\n')
		choice := strings.ToLower(strings.TrimSpace(input))

		if choice == "y" || choice == "yes" {
			utils.Info(i18n.T("continuing_download"))
			if onContinue != nil {
				onContinue(a.Config.DownloadState.Tasks, a.Config.DownloadState.FormatStrategy, a.Config.DownloadState.SelectionMode)
			}
		} else {
			utils.Info(i18n.T("download_cancelled"))
			config.ClearDownloadState(a.Config)
		}
	}
}

func convertToConfigStrategy(spiderStrategy *spider.FilterStrategy) *config.FormatStrategy {
	if spiderStrategy == nil {
		return nil
	}
	return &config.FormatStrategy{
		Mode:             spiderStrategy.Mode,
		PriorityFormats:  spiderStrategy.PriorityFormats,
		ManualSelections: spiderStrategy.ManualSelections,
		IncludeFormats:   spiderStrategy.IncludeFormats,
		OnlyFormats:      spiderStrategy.OnlyFormats,
	}
}
