package utils

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	// chunkSize 固定分块大小（5MB），按此大小切分为大量小块
	chunkSize = 5 * 1024 * 1024
	// defaultBufferSize 默认缓冲区大小（8MB）
	defaultBufferSize = 8 * 1024 * 1024
	// partFileSuffix .part 文件后缀
	partFileSuffix = ".part"
)

var (
	defaultUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/86.0.4240.198 Safari/537.36"
)

// PartFileMeta .part 文件的元数据
type PartFileMeta struct {
	TotalSize int64          `json:"total_size"`
	Blocks    []*BlockStatus `json:"blocks"`
}

// BlockStatus 单个 chunk 的持久化状态
type BlockStatus struct {
	Begin int64 `json:"begin"`
	End   int64 `json:"end"`
	Done  bool  `json:"done"`
}

// chunkTask worker 消费的子任务单元
type chunkTask struct {
	blockIndex  int
	beginOffset int64
	endOffset   int64
}

type MultiThreadDownloader struct {
	Url         string
	SavePath    string
	FileName    string
	FullPath    string
	Client      *http.Client
	Headers     map[string]string
	ThreadCount int
	BufferSize  int // 缓冲区大小（字节），从配置文件读取
	ProgressBar *ProgressBar
	OnFailure   func(url, savePath, fileName string, err error)
	RetryCount  int

	partMeta   *PartFileMeta
	partFileMu sync.Mutex
}

// progressWriter 封装 io.Writer 以更新进度条
type progressWriter struct {
	w   io.Writer
	bar *ProgressBar
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.w.Write(p)
	if n > 0 && pw.bar != nil {
		pw.bar.Add(int64(n))
	}
	return n, err
}

func NewDownloader(url string, path string, name string, threadCount int, bufferSize int, headers map[string]string) *MultiThreadDownloader {
	// 如果bufferSize为0，使用默认值
	if bufferSize <= 0 {
		bufferSize = defaultBufferSize
	}
	// 如果threadCount小于1，使用1
	if threadCount < 1 {
		threadCount = 1
	}
	return &MultiThreadDownloader{
		Url:         url,
		SavePath:    path,
		FileName:    name,
		FullPath:    filepath.Join(path, name),
		Client:      Client.Get().(*http.Client),
		Headers:     headers,
		ThreadCount: threadCount,
		BufferSize:  bufferSize,
	}
}

func (m *MultiThreadDownloader) Download() error {
	// 确保Client在下载完成后被释放
	defer Client.Put(m.Client)

	// 尝试加载 .part 文件
	needInit := true
	if meta, err := m.loadPartFile(); err == nil && meta != nil {
		if err := m.resumeFromPartFile(meta); err == nil {
			needInit = false
		} else {
			// 续传初始化失败，删除 .part 文件从头下载
			m.removePartFile()
			_ = os.Remove(m.FullPath)
		}
	}

	if needInit {
		supported, err := m.initNewDownload()
		if err != nil {
			return err
		}
		if !supported {
			return m.fallbackDownload()
		}
	}

	// 执行多线程下载（生产者-消费者模型）
	if err := m.multiThreadDownload(); err != nil {
		return err
	}

	m.removePartFile()
	if m.ProgressBar != nil {
		m.ProgressBar.Finish()
	}
	return nil
}

// partFilePath 返回 .part 文件路径
func (m *MultiThreadDownloader) partFilePath() string {
	return m.FullPath + partFileSuffix
}

// loadPartFile 读取并解析 .part 文件
func (m *MultiThreadDownloader) loadPartFile() (*PartFileMeta, error) {
	data, err := os.ReadFile(m.partFilePath())
	if err != nil {
		return nil, err
	}
	var meta PartFileMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// savePartFile 原子写入 .part 文件（写 tmp + rename）
func (m *MultiThreadDownloader) savePartFile() error {
	m.partFileMu.Lock()
	defer m.partFileMu.Unlock()

	data, err := json.Marshal(m.partMeta)
	if err != nil {
		return err
	}
	tmpPath := m.partFilePath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, m.partFilePath())
}

// removePartFile 删除 .part 文件
func (m *MultiThreadDownloader) removePartFile() {
	os.Remove(m.partFilePath())
}

// initNewDownload 探测服务器 Range 支持并初始化分块下载
// 返回 (true, nil) 表示支持 Range 且 blocks 已初始化
// 返回 (false, nil) 表示不支持 Range，需要走 fallbackDownload
func (m *MultiThreadDownloader) initNewDownload() (bool, error) {
	req, err := http.NewRequest("GET", m.Url, nil)
	if err != nil {
		return false, err
	}

	for k, v := range m.Headers {
		req.Header.Set(k, v)
	}
	if _, ok := m.Headers["User-Agent"]; !ok {
		req.Header["User-Agent"] = []string{defaultUA}
	}
	// 尝试获取文件头信息或探测 Range 支持
	req.Header.Set("range", "bytes=0-")

	resp, err := m.Client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, errors.New("response status unsuccessful: " + strconv.FormatInt(int64(resp.StatusCode), 10))
	}

	// 如果服务器直接返回 200 (不支持 Range) 或者没有 ContentLength，直接流式下载
	if resp.StatusCode == 200 {
		return false, nil
	}

	if resp.StatusCode == 206 {
		// 从 Content-Range 解析总大小
		totalSize, err := ParseContentRange(resp.Header.Get("Content-Range"))
		if err != nil {
			return false, err
		}

		// 关闭探测响应的 body
		resp.Body.Close()

		// 按固定 chunkSize 切分
		blocks := make([]*BlockStatus, 0, totalSize/chunkSize+1)
		var offset int64
		for offset < totalSize {
			end := offset + chunkSize - 1
			if end >= totalSize {
				end = totalSize - 1
			}
			blocks = append(blocks, &BlockStatus{
				Begin: offset,
				End:   end,
				Done:  false,
			})
			offset = end + 1
		}

		// 预分配文件
		file, err := os.OpenFile(m.FullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
		if err != nil {
			return false, err
		}
		if err := file.Truncate(totalSize); err != nil {
			file.Close()
			return false, err
		}
		file.Close()

		// 构建 .part 元数据并持久化
		m.partMeta = &PartFileMeta{
			TotalSize: totalSize,
			Blocks:    blocks,
		}
		if err := m.savePartFile(); err != nil {
			return false, err
		}

		m.ProgressBar = NewProgressBar(ProgressBarOptions{Total: totalSize, Prefix: m.FileName})
		return true, nil
	}

	return false, errors.New("unknown status code")
}

// fallbackDownload 不支持 Range 时的流式下载
func (m *MultiThreadDownloader) fallbackDownload() error {
	file, err := os.OpenFile(m.FullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriterSize(file, m.BufferSize)
	defer writer.Flush()

	req, err := http.NewRequest("GET", m.Url, nil)
	if err != nil {
		return err
	}

	for k, v := range m.Headers {
		req.Header.Set(k, v)
	}
	if _, ok := m.Headers["User-Agent"]; !ok {
		req.Header["User-Agent"] = []string{defaultUA}
	}

	resp, err := m.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("response status unsuccessful: " + strconv.FormatInt(int64(resp.StatusCode), 10))
	}

	if resp.ContentLength > 0 {
		m.ProgressBar = NewProgressBar(ProgressBarOptions{Total: resp.ContentLength, Prefix: m.FileName})
	}

	pw := &progressWriter{w: writer, bar: m.ProgressBar}
	buf := make([]byte, m.BufferSize)
	if _, err := io.CopyBuffer(pw, resp.Body, buf); err != nil {
		return err
	}

	if m.ProgressBar != nil {
		m.ProgressBar.Finish()
	}
	return nil
}

// resumeFromPartFile 从 .part 文件恢复下载
func (m *MultiThreadDownloader) resumeFromPartFile(meta *PartFileMeta) error {
	// 校验本地文件大小
	localSize, err := GetFileSize(m.FullPath)
	if err != nil || localSize != meta.TotalSize {
		return fmt.Errorf("local file size mismatch: local=%d, expected=%d", localSize, meta.TotalSize)
	}

	m.partMeta = meta

	var completedSize int64
	for _, b := range meta.Blocks {
		if b.Done {
			completedSize += b.End - b.Begin + 1
		}
	}

	m.ProgressBar = NewProgressBar(ProgressBarOptions{Total: meta.TotalSize, Offset: completedSize, Prefix: m.FileName})
	Infof("续传下载: %s (已完成 %s)", m.FileName, FormatBytes(completedSize))
	return nil
}

// multiThreadDownload 生产者-消费者模型执行多线程下载
func (m *MultiThreadDownloader) multiThreadDownload() error {
	chunks := make(chan *chunkTask, len(m.partMeta.Blocks))

	// 生产者：将未完成的块投入 channel
	for i, block := range m.partMeta.Blocks {
		if block.Done {
			continue
		}
		chunks <- &chunkTask{
			blockIndex:  i,
			beginOffset: block.Begin,
			endOffset:   block.End,
		}
	}
	close(chunks)

	// 消费者：ThreadCount 个 worker
	var wg sync.WaitGroup
	wg.Add(m.ThreadCount)
	var lastErr error
	var errOnce sync.Once

	for i := 0; i < m.ThreadCount; i++ {
		go func() {
			defer wg.Done()
			for chunk := range chunks {
				if err := m.downloadChunk(chunk); err != nil {
					errOnce.Do(func() { lastErr = err })
				}
			}
		}()
	}

	wg.Wait()
	return lastErr
}

// downloadChunk 下载单个 chunk
func (m *MultiThreadDownloader) downloadChunk(chunk *chunkTask) error {
	file, err := os.OpenFile(m.FullPath, os.O_WRONLY|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}
	defer file.Close()

	// 定位到该块的起始位置
	if _, err := file.Seek(chunk.beginOffset, io.SeekStart); err != nil {
		return err
	}

	// 使用配置的BufferSize来优化IO性能
	writer := bufio.NewWriterSize(file, m.BufferSize)
	defer writer.Flush()

	req, err := http.NewRequest("GET", m.Url, nil)
	if err != nil {
		return err
	}

	for k, v := range m.Headers {
		req.Header.Set(k, v)
	}
	if _, ok := m.Headers["User-Agent"]; !ok {
		req.Header["User-Agent"] = []string{defaultUA}
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunk.beginOffset, chunk.endOffset))

	resp, err := m.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("response status unsuccessful: " + strconv.FormatInt(int64(resp.StatusCode), 10))
	}

	if resp.StatusCode == http.StatusOK && chunk.beginOffset > 0 {
		return errors.New("server does not support Range requests")
	}

	// 使用 io.LimitReader 裁剪 + progressWriter 自动更新进度
	chunkSize := chunk.endOffset - chunk.beginOffset + 1
	pw := &progressWriter{w: writer, bar: m.ProgressBar}
	// 使用大buffer来减少系统调用，提升VPS等网络环境下的性能
	buf := make([]byte, m.BufferSize)
	if _, err := io.CopyBuffer(pw, io.LimitReader(resp.Body, chunkSize), buf); err != nil {
		return err
	}

	// chunk 完成后标记并同步
	m.markBlockDone(chunk.blockIndex)
	_ = m.savePartFile()

	return nil
}

// markBlockDone 标记指定 block 为已完成
func (m *MultiThreadDownloader) markBlockDone(index int) {
	m.partFileMu.Lock()
	defer m.partFileMu.Unlock()

	if index < len(m.partMeta.Blocks) {
		m.partMeta.Blocks[index].Done = true
	}
}
