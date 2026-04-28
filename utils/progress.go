package utils

import (
	"fmt"
	"io"

	"github.com/schollz/progressbar/v3"
)

type ProgressBarOptions struct {
	Total  int64
	Offset int64
	Prefix string
}

type ProgressBar struct {
	bar      *progressbar.ProgressBar
	finished bool
}

func NewProgressBar(opts ProgressBarOptions) *ProgressBar {
	bar := progressbar.NewOptions64(
		opts.Total,
		progressbar.OptionSetDescription(opts.Prefix),
		progressbar.OptionSetWriter(io.Discard), // 先禁用输出
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(40),
		progressbar.OptionThrottle(100), // 100ms更新一次
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() {
			fmt.Println() // 完成后换行
		}),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	// 重新启用输出到 stderr (标准错误输出)
	bar.RenderBlank()
	bar = progressbar.NewOptions64(
		opts.Total,
		progressbar.OptionSetDescription(opts.Prefix),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(40),
		progressbar.OptionThrottle(100),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() {
			fmt.Println()
		}),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	pb := &ProgressBar{
		bar:      bar,
		finished: false,
	}

	if opts.Offset > 0 {
		pb.Add(opts.Offset)
	}
	return pb
}

func (pb *ProgressBar) Add(n int64) {
	if pb.finished {
		return
	}
	_ = pb.bar.Add64(n)
}

func (pb *ProgressBar) Finish() {
	if pb.finished {
		return
	}
	pb.finished = true
	_ = pb.bar.Finish()
}
