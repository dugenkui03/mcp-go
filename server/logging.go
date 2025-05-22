package server

import (
	"context"
	"fmt"
	"github.com/mark3labs/mcp-go/mcp"
	"sync/atomic"
)

type serverLog struct {
	loggingLevel atomic.Value
	server       *MCPServer
}

func (logger *serverLog) SetLogLevel(level mcp.LoggingLevel) {
	logger.loggingLevel.Store(level)
}

func (logger *serverLog) GetLogLevel() mcp.LoggingLevel {
	level := logger.loggingLevel.Load()
	if level == nil {
		return mcp.LoggingLevelError
	}
	return level.(mcp.LoggingLevel)
}

func (logger *serverLog) Log(ctx context.Context, level mcp.LoggingLevel, message string) {
	if logger == nil {
		return
	}

	// only send log if the log level is high enough
	if logger.loggingLevel.Load().(mcp.LoggingLevel) < level {
		return
	}

	_ = logger.server.SendNotificationToClient(ctx, mcp.MethodNotificationMessage, map[string]any{
		"level":   level,
		"logger":  fmt.Sprintf("mcp-server-%s-logger", logger.server.name),
		"message": message,
	})
}

func (logger *serverLog) Debug(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelDebug, message)
}

func (logger *serverLog) Info(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelInfo, message)
}

func (logger *serverLog) Notice(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelNotice, message)
}

func (logger *serverLog) Warning(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelWarning, message)
}

func (logger *serverLog) Error(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelError, message)
}

func (logger *serverLog) Critical(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelCritical, message)
}

func (logger *serverLog) Alert(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelAlert, message)
}

func (logger *serverLog) Emergency(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelEmergency, message)
}
