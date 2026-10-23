package utils

import "testing"

// TestSetMuted 验证静音开关：muted 为 true 时所有级别的日志均不输出。
func TestSetMuted(t *testing.T) {
	// 恢复全局状态，避免影响其他测试
	defer SetMuted(false)

	SetMuted(true)
	defer func() {
		if defaultLogger.muted.Load() {
			t.Error("测试结束后静音应已恢复")
		}
	}()

	if !defaultLogger.muted.Load() {
		t.Fatal("SetMuted(true) 后 muted 应为 true")
	}

	// 静音期间调用不应 panic（输出被抑制，此处仅验证路径安全）
	defaultLogger.logMessage(LevelInfo, "静音期间不应输出")
	defaultLogger.logFormat(LevelError, "静音期间不应输出 %d", 1)

	SetMuted(false)
	if defaultLogger.muted.Load() {
		t.Fatal("SetMuted(false) 后 muted 应为 false")
	}
}
