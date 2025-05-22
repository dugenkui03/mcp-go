package mcp

import (
	"context"
)

type Logger interface {
	// SetLogLevel sets the minimum log level
	SetLogLevel(level LoggingLevel)
	// GetLogLevel retrieves the minimum log level
	GetLogLevel() LoggingLevel
	Log(ctx context.Context, level LoggingLevel, message string)
	Debug(ctx context.Context, message string)
	Info(ctx context.Context, message string)
	Notice(ctx context.Context, message string)
	Warning(ctx context.Context, message string)
	Error(ctx context.Context, message string)
	Critical(ctx context.Context, message string)
	Alert(ctx context.Context, message string)
	Emergency(ctx context.Context, message string)
}
