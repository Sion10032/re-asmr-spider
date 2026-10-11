# AGENTS.md — re-asmr-spider

## 开发环境

- Go 1.24+；开发环境用 Nix flake（`nix develop` 或 direnv），GOPATH 在项目内 `.go/`
- 无必要时不新增外部依赖；确需新增前必须先与用户确认

## 常用命令

```bash
go build -o re-asmr-spider            # 构建
go run .                              # 运行（交互模式）
./re-asmr-spider -download RJ123456   # CLI 模式
go test ./...                         # 测试
go fmt ./... && go vet ./...          # 提交前必跑
```

## 目录结构

```
app/      应用控制
cli/      命令行
config/   配置
i18n/     国际化
spider/   ASMR API 客户端与下载逻辑
ui/       交互 TUI
utils/    日志、下载器等工具
version/  版本信息
```

## 代码约定

- 注释一律中文；`go fmt` 格式，tab 缩进，行宽约 120
- import 分三组：标准库 / 第三方 / 内部包（`re-asmr-spider/...`），组间空行
- 错误向上返回并用 `fmt.Errorf("%w", err)` 包装；仅顶层打印并退出
- 日志统一用 `utils.Debug/Info/Success/Warning/Error`（含格式化版本）
- 共享状态用 `sync.Mutex`；并发任务用 worker pool；资源一律 `defer` 释放
- 测试为表驱动，放在实现文件旁（`xxx_test.go`）

## 注意

- 不得记录或暴露凭据
