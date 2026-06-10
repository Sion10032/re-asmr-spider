package cli

import (
	"fmt"
	"os"

	"re-asmr-spider/i18n"
)

// PrintHelp 打印帮助信息
func PrintHelp() {
	fmt.Printf(`%s - 简单高效的 ASMR 音声下载工具

用法:
  %s [选项]

选项:
  -download string
        要下载的RJ号，多个用逗号分隔 (例: RJ123456,RJ789012)

  -config string
        配置文件路径 (默认: config.json)

  -account string
        ASMR.one账号 (覆盖配置文件)

  -password string
        ASMR.one密码 (覆盖配置文件)

  -max-task int
        最大并发任务数 (默认使用配置文件)

  -max-thread int
        单文件下载线程数 (默认使用配置文件)

  -max-retry int
        最大重试次数 (默认使用配置文件)

  -buffer-size int
        缓冲区大小(MB)，范围 1-64 (默认使用配置文件)

  -proxy string
        HTTP/HTTPS代理地址 (例: http://127.0.0.1:7890)

  -format-priority string
        格式优先级，逗号分隔 (例: flac,wav,mp3)
        仅在"同名文件存在多种格式"时生效：只下载优先级最高的那个
        注意：它不会排除没有同名对的其它格式文件；要硬过滤请用 -only-formats
        (交互模式会逐个询问用户选择)

  -only-formats string
        硬白名单：全局只下载指定扩展名的文件，逗号分隔 (例: wav,flac)
        其余文件（含其它音频格式、封面、字幕、文本等）一律丢弃
        如需保留封面/字幕等，配合 -include-formats 加回

  -include-formats string
        额外包含的扩展名，逗号分隔 (例: lrc,jpg)
        冲突解决后，额外下载所有指定扩展名的文件
        可与 -format-priority、-only-formats 配合使用

  -version
        显示版本信息

  -help
        显示此帮助信息

示例:
  # 交互模式（默认）
  %s

  # 下载单个RJ号
  %s -download RJ123456

  # 下载多个RJ号
  %s -download RJ123456,RJ789012,RJ345678

  # 使用自定义配置和账号
  %s -download RJ123456 -account user@example.com -password mypass

  # 设置下载参数
  %s -download RJ123456 -max-task 5 -max-thread 16 -buffer-size 16

  # 使用代理下载
  %s -download RJ123456 -proxy http://127.0.0.1:7890

  # 指定配置文件
  %s -config /path/to/config.json -download RJ123456

  # 同名文件存在多格式时优先选FLAC（不会排除无同名对的其它格式）
  %s -download RJ123456 -format-priority flac,wav,mp3

  # 只下载wav，其余（含mp3、封面、字幕）全部丢弃（硬过滤）
  %s -download RJ123456 -only-formats wav

  # 只下载wav，并保留封面和字幕
  %s -download RJ123456 -only-formats wav -include-formats jpg,lrc

  # 额外下载所有txt和jpg文件
  %s -download RJ123456 -include-formats txt,jpg

  # 组合使用：冲突时选择FLAC，并额外下载所有图片和文本
  %s -download RJ123456 -format-priority flac -include-formats txt,jpg,png

更多信息:
  项目主页: https://github.com/reuAC/re-asmr-spider
  数据来源: https://asmr.one
`,
		i18n.AppName(),
		os.Args[0],
		os.Args[0],
		os.Args[0],
		os.Args[0],
		os.Args[0],
		os.Args[0],
		os.Args[0],
		os.Args[0],
		os.Args[0],
		os.Args[0],
		os.Args[0],
		os.Args[0],
		os.Args[0],
	)
}
