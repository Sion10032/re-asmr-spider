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
// 供 DownloadWithMonitorAndFormatSelector 与 SelectionSession.Finish 共用。
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

// TaskPreparation 单个任务的选择前置数据：由 SelectionSession.Prepare 拉取并判定，
// 供 TUI 会话分流（沿用/需选择/失败）与后续落盘使用。
type TaskPreparation struct {
	Task        string                 // 任务输入原串（RJ/VJ/纯数字）
	BasePath    string                 // 作品下载根目录（"downloads/"+任务原串，与 spider 侧 key 口径一致）
	Tracks      []spider.Track         // 完整文件树
	Analysis    *spider.FormatAnalysis // 格式统计（确认后 FilesForKeys 落盘用）
	InitialKeys map[string]bool        // 重选时的预勾选 key 集；nil 表示全选
	Reused      bool                   // true=有可用记录直接沿用（无需打开选择器）
	ReusedKeys  []string               // 沿用时的文件 key 列表
	ReusedCount int                    // 沿用文件数（TUI 摘要展示用）
	Missing     int                    // 沿用时已不存在的文件数（>0 时展示警告）
	Stale       bool                   // 有记录但与当前文件树完全不符，需重新选择
	RecordErr   error                  // 选择记录读取失败（按无记录处理，TUI 呈现警告）
	Err         error                  // 文件树拉取失败（TUI 呈现并跳过该任务）
}

// SelectionSession 手动文件树选择模式的分阶段会话：
// StartSelectionSession 建立登录与监控 → Prepare/Confirm/Reuse 由 TUI 会话逐任务驱动 → Finish 执行下载。
// 拆分为阶段是为了让多作品的整个选择流程运行在单个全屏 TUI 内，作品间切换不再回显主终端内容。
type SelectionSession struct {
	a             *App
	client        *spider.ASMRClient
	reselect      bool
	strategies    map[string]*spider.FilterStrategy
	selectedTasks []string
}

// StartSelectionSession 启动选择会话：保存选择模式下载状态、登录、启动活动监控。
// 登录失败时打印错误、清除状态并返回 nil。
func (a *App) StartSelectionSession(tasks []string, reselect bool) *SelectionSession {
	if err := config.SaveDownloadStateForSelection(a.Config, tasks); err != nil {
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
		return nil
	}

	// 启动活动监控
	utils.GlobalMonitor.Start()

	return &SelectionSession{
		a:             a,
		client:        c,
		reselect:      reselect,
		strategies:    make(map[string]*spider.FilterStrategy),
		selectedTasks: make([]string, 0, len(tasks)),
	}
}

// Prepare 拉取指定任务的文件树并判定沿用/需选择。含网络请求会阻塞，由 TUI 在加载态中调用。
// 本方法不打印日志：会话 TUI 活跃期间日志已静音，信息经返回值由 TUI 呈现。
func (s *SelectionSession) Prepare(task string) TaskPreparation {
	prep := TaskPreparation{Task: task, BasePath: "downloads/" + task}

	tracks, err := s.client.GetVoiceTracks(task)
	if err != nil {
		prep.Err = err
		return prep
	}
	prep.Tracks = tracks
	prep.Analysis = spider.AnalyzeFormats(tracks, prep.BasePath)

	// 读取选择记录；记录损坏时按无可用记录处理（标记后走选择路径）
	rec, exists, err := spider.LoadSelection(prep.BasePath)
	if err != nil {
		prep.RecordErr = err
	}

	// 有记录且未强制重选：尝试沿用
	if exists && !s.reselect && rec != nil {
		matched, missing := spider.MatchSelection(rec, prep.Analysis)
		if len(matched) > 0 {
			prep.Reused = true
			prep.ReusedCount = len(matched)
			prep.Missing = missing
			for _, fi := range matched {
				prep.ReusedKeys = append(prep.ReusedKeys, spider.TrackKey(fi.Track.Hash, fi.Path, fi.Track.Title))
			}
			return prep
		}
		// 记录与当前文件树完全不符：标记后走重新选择路径
		prep.Stale = true
	}

	// 重选时把上次记录的 key 集作为预勾选传入
	if s.reselect && exists && rec != nil {
		prep.InitialKeys = make(map[string]bool, len(rec.Files))
		for _, sf := range rec.Files {
			prep.InitialKeys[sf.Key] = true
		}
	}
	return prep
}

// Reuse 沿用已保存选择：直接建立该任务的过滤策略并纳入下载列表。
func (s *SelectionSession) Reuse(prep TaskPreparation) {
	s.strategies[prep.Task] = spider.NewFileManualStrategy(prep.ReusedKeys)
	s.selectedTasks = append(s.selectedTasks, prep.Task)
}

// Confirm 确认选择：立即落盘选择记录（防后续中断丢失）并建立过滤策略。
// 落盘失败的错误返回给 TUI 呈现（会话期间日志静音）；策略照常建立，
// 与原流程「警告后继续下载已选文件」的口径一致。
func (s *SelectionSession) Confirm(prep TaskPreparation, keys map[string]bool) error {
	files := spider.FilesForKeys(prep.Analysis, keys)
	if err := spider.SaveSelection(prep.BasePath, prep.Task, files); err != nil {
		return err
	}
	keyList := make([]string, 0, len(keys))
	for k := range keys {
		keyList = append(keyList, k)
	}
	s.strategies[prep.Task] = spider.NewFileManualStrategy(keyList)
	s.selectedTasks = append(s.selectedTasks, prep.Task)
	return nil
}

// Cancel 中止会话：停止活动监控。已保存的选择记录与下载状态保留，便于之后继续。
func (s *SelectionSession) Cancel() {
	utils.GlobalMonitor.Stop()
}

// Finish 结束选择并执行下载循环（超时重试，与 DownloadWithMonitorAndFormatSelector 同构）；
// 全部任务被跳过（拉取失败/未选中文件）时不进入下载循环，直接收尾。
// 返回整个流程是否成功：与现有 CLI 下载路径口径一致（无失败文件视为成功）。
func (s *SelectionSession) Finish(waitForInput bool) bool {
	utils.GlobalMonitor.Stop()

	// 与现有 CLI 下载路径口径一致：无失败文件视为成功；全部任务被跳过时未发生下载，视为失败
	success := false

	if len(s.selectedTasks) > 0 {
		var result *DownloadResult
		for {
			// 启动超时检测goroutine
			stopMonitor, timeoutDetected := startTimeoutWatcher()

			// 执行下载（仅含成功建立策略的任务）
			result = s.a.Download(DownloadOptions{
				Tasks:           s.selectedTasks,
				ShowProgress:    true,
				WaitForInput:    false,
				TimeoutDetected: timeoutDetected,
				TaskFilters:     s.strategies,
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
	if err := config.ClearDownloadState(s.a.Config); err != nil {
		utils.Error(i18n.T("clear_download_state_failed", err))
	}

	// 等待用户输入
	if waitForInput {
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
