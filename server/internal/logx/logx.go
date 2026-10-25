// Package logx 是 NeuxBot 后端唯一的日志输出边界。
//
// ginx/logx 当前会在每次取 logger 时写包级 modulePath，无法安全地被并发调用。
// 这里保留 ginx/logx 的配置、格式和调用语义，并串行化入口，避免业务并发触发数据竞争。
package logx

import (
	"sync"

	ginxlogx "github.com/shrewx/ginx/pkg/logx"
)

var outputMutex sync.Mutex

// Debug 输出调试日志。
func Debug(args ...any) {
	outputMutex.Lock()
	defer outputMutex.Unlock()
	ginxlogx.Debug(args...)
}

// Info 输出普通日志。
func Info(args ...any) {
	outputMutex.Lock()
	defer outputMutex.Unlock()
	ginxlogx.Info(args...)
}

// Warn 输出警告日志。
func Warn(args ...any) {
	outputMutex.Lock()
	defer outputMutex.Unlock()
	ginxlogx.Warn(args...)
}

// Error 输出错误日志。
func Error(args ...any) {
	outputMutex.Lock()
	defer outputMutex.Unlock()
	ginxlogx.Error(args...)
}

// Infof 按格式输出普通日志。
func Infof(format string, args ...any) {
	outputMutex.Lock()
	defer outputMutex.Unlock()
	ginxlogx.Infof(format, args...)
}

// Warnf 按格式输出警告日志。
func Warnf(format string, args ...any) {
	outputMutex.Lock()
	defer outputMutex.Unlock()
	ginxlogx.Warnf(format, args...)
}

// Errorf 按格式输出错误日志。
func Errorf(format string, args ...any) {
	outputMutex.Lock()
	defer outputMutex.Unlock()
	ginxlogx.Errorf(format, args...)
}
