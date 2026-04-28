package utils

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || errors.Is(err, os.ErrExist)
}

// GetFileSize 获取本地文件大小
func GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// GetRemoteFileSize 获取远程文件大小
func GetRemoteFileSize(url string, headers map[string]string) (int64, error) {
	client := Client.Get().(*http.Client)
	defer Client.Put(client)

	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return 0, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if _, ok := headers["User-Agent"]; !ok {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/86.0.4240.198 Safari/537.36")
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return 0, errors.New("failed to get remote file size, status: " + strconv.Itoa(resp.StatusCode))
	}

	return resp.ContentLength, nil
}

// ParseContentRange 从 Content-Range 响应头解析文件总大小
// 支持: "bytes 0-5242879/104857600" 和 "bytes */104857600"
func ParseContentRange(header string) (int64, error) {
	if header == "" {
		return 0, errors.New("empty Content-Range header")
	}
	slashIdx := strings.LastIndex(header, "/")
	if slashIdx < 0 {
		return 0, errors.New("invalid Content-Range format: " + header)
	}
	totalStr := header[slashIdx+1:]
	if totalStr == "*" {
		return 0, errors.New("unknown total size in Content-Range")
	}
	return strconv.ParseInt(totalStr, 10, 64)
}

// FormatBytes 将字节数格式化为人类可读字符串
func FormatBytes(n int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case n >= GB:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(GB))
	case n >= MB:
		return fmt.Sprintf("%.2f MB", float64(n)/float64(MB))
	case n >= KB:
		return fmt.Sprintf("%.2f KB", float64(n)/float64(KB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
