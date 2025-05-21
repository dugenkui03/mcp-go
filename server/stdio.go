package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/mark3labs/mcp-go/mcp"
)

// StdioContextFunc is a function that takes an existing context and returns
// a potentially modified context.
// This can be used to inject context values from environment variables,
// for example.
type StdioContextFunc func(ctx context.Context) context.Context

// StdioServer wraps a MCPServer and handles stdio communication.
// It provides a simple way to create command-line MCP servers that
// communicate via standard input/output streams using JSON-RPC messages.
type StdioServer struct {
	server      *MCPServer
	errLogger   *log.Logger
	contextFunc StdioContextFunc
}

// StdioOption defines a function type for configuring StdioServer
type StdioOption func(*StdioServer)

// WithErrorLogger sets the error logger for the server
func WithErrorLogger(logger *log.Logger) StdioOption {
	return func(s *StdioServer) {
		s.errLogger = logger
	}
}

// WithStdioContextFunc sets a function that will be called to customise the context
// to the server. Note that the stdio server uses the same context for all requests,
// so this function will only be called once per server instance.
func WithStdioContextFunc(fn StdioContextFunc) StdioOption {
	return func(s *StdioServer) {
		s.contextFunc = fn
	}
}

// stdioSession is a static client session, since stdio has only one client.
type stdioSession struct {
	notifications chan mcp.JSONRPCNotification // note 发送消息的通道
	initialized   atomic.Bool
	logger        stdioLogger
}

func (s *stdioSession) SessionID() string {
	return "stdio"
}

func (s *stdioSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return s.notifications
}

func (s *stdioSession) Initialize() {
	s.initialized.Store(true)
}

func (s *stdioSession) Initialized() bool {
	return s.initialized.Load()
}

func (s *stdioSession) GetLogger() Logger {
	return &s.logger
}

type stdioLogger struct {
	loggingLevel atomic.Value
	server       *MCPServer
}

func (logger *stdioLogger) SetLogLevel(level mcp.LoggingLevel) {
	logger.loggingLevel.Store(level)
}

func (logger *stdioLogger) GetLogLevel() mcp.LoggingLevel {
	level := logger.loggingLevel.Load()
	if level == nil {
		return mcp.LoggingLevelError
	}
	return level.(mcp.LoggingLevel)
}

func (logger *stdioLogger) Log(ctx context.Context, level mcp.LoggingLevel, message string) {
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

func (logger *stdioLogger) Debug(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelDebug, message)
}

func (logger *stdioLogger) Info(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelInfo, message)
}

func (logger *stdioLogger) Notice(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelNotice, message)
}

func (logger *stdioLogger) Warning(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelWarning, message)
}

func (logger *stdioLogger) Error(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelError, message)
}

func (logger *stdioLogger) Critical(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelCritical, message)
}

func (logger *stdioLogger) Alert(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelAlert, message)
}

func (logger *stdioLogger) Emergency(ctx context.Context, message string) {
	logger.Log(ctx, mcp.LoggingLevelEmergency, message)
}

var (
	_ ClientSession      = (*stdioSession)(nil)
	_ SessionWithLogging = (*stdioSession)(nil)
)

var stdioSessionInstance = stdioSession{
	notifications: make(chan mcp.JSONRPCNotification, 100),
}

// NewStdioServer creates a new stdio server wrapper around an MCPServer.
// It initializes the server with a default error logger that discards all output.
func NewStdioServer(server *MCPServer) *StdioServer {
	return &StdioServer{
		server: server,
		errLogger: log.New(
			os.Stderr,
			"",
			log.LstdFlags,
		), // Default to discarding logs
	}
}

// SetErrorLogger configures where error messages from the StdioServer are logged.
// The provided logger will receive all error messages generated during server operation.
func (s *StdioServer) SetErrorLogger(logger *log.Logger) {
	s.errLogger = logger
}

// SetContextFunc sets a function that will be called to customise the context
// to the server. Note that the stdio server uses the same context for all requests,
// so this function will only be called once per server instance.
func (s *StdioServer) SetContextFunc(fn StdioContextFunc) {
	s.contextFunc = fn
}

// handleNotifications continuously processes notifications from the session's notification channel
// and writes them to the provided output. It runs until the context is cancelled.
// Any errors encountered while writing notifications are logged but do not stop the handler.
func (s *StdioServer) handleNotifications(ctx context.Context, stdout io.Writer) {
	for {
		select {
		case notification := <-stdioSessionInstance.notifications:
			if err := s.writeResponse(notification, stdout); err != nil {
				s.errLogger.Printf("Error writing notification: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// processInputStream continuously reads and processes messages from the input stream.
// It handles EOF gracefully as a normal termination condition.
// The function returns when either:
// - The context is cancelled (returns context.Err())
// - EOF is encountered (returns nil)
// - An error occurs while reading or processing messages (returns the error)
func (s *StdioServer) processInputStream(ctx context.Context, reader *bufio.Reader, stdout io.Writer) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		line, err := s.readNextLine(ctx, reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			s.errLogger.Printf("Error reading input: %v", err)
			return err
		}

		if err := s.processMessage(ctx, line, stdout); err != nil {
			if err == io.EOF {
				return nil
			}
			s.errLogger.Printf("Error handling message: %v", err)
			return err
		}
	}
}

// readNextLine reads a single line from the input reader in a context-aware manner.
// It uses channels to make the read operation cancellable via context.
// Returns the read line and any error encountered. If the context is cancelled,
// returns an empty string and the context's error. EOF is returned when the input
// stream is closed.
func (s *StdioServer) readNextLine(ctx context.Context, reader *bufio.Reader) (string, error) {
	readChan := make(chan string, 1)
	errChan := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-done:
			return
		default:
			line, err := reader.ReadString('\n')
			if err != nil {
				select {
				case errChan <- err:
				case <-done:
				}
				return
			}
			select {
			case readChan <- line:
			case <-done:
			}
			return
		}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-errChan:
		return "", err
	case line := <-readChan:
		return line, nil
	}
}

// Listen starts listening for JSON-RPC messages on the provided input and writes responses to the provided output.
// It runs until the context is cancelled or an error occurs.
// Returns an error if there are issues with reading input or writing output.
func (s *StdioServer) Listen(
	ctx context.Context,
	stdin io.Reader,
	stdout io.Writer,
) error {
	// Set a static client context since stdio only has one client
	if err := s.server.RegisterSession(ctx, &stdioSessionInstance); err != nil {
		return fmt.Errorf("register session: %w", err)
	}
	defer s.server.UnregisterSession(ctx, stdioSessionInstance.SessionID())
	// note WithContext 是特么自定义方法，不是 golang 的
	// note 一个client链接对应一个 session
	ctx = s.server.WithContext(ctx, &stdioSessionInstance)

	// Add in any custom context.
	if s.contextFunc != nil {
		ctx = s.contextFunc(ctx)
	}

	reader := bufio.NewReader(stdin)

	// Start notification handler
	go s.handleNotifications(ctx, stdout)
	return s.processInputStream(ctx, reader, stdout)
}

// processMessage handles a single JSON-RPC message and writes the response.
// It parses the message, processes it through the wrapped MCPServer, and writes any response.
// Returns an error if there are issues with message processing or response writing.
func (s *StdioServer) processMessage(
	ctx context.Context,
	line string,
	writer io.Writer,
) error {
	// Parse the message as raw JSON
	var rawMessage json.RawMessage
	if err := json.Unmarshal([]byte(line), &rawMessage); err != nil {
		response := createErrorResponse(nil, mcp.PARSE_ERROR, "Parse error")
		return s.writeResponse(response, writer)
	}

	// Handle the message using the wrapped server
	response := s.server.HandleMessage(ctx, rawMessage)

	// Only write response if there is one (not for notifications)
	if response != nil {
		if err := s.writeResponse(response, writer); err != nil {
			return fmt.Errorf("failed to write response: %w", err)
		}
	}

	return nil
}

// writeResponse marshals and writes a JSON-RPC response message followed by a newline.
// Returns an error if marshaling or writing fails.
func (s *StdioServer) writeResponse(
	response mcp.JSONRPCMessage,
	writer io.Writer,
) error {
	responseBytes, err := json.Marshal(response)
	if err != nil {
		return err
	}

	// Write response followed by newline
	if _, err := fmt.Fprintf(writer, "%s\n", responseBytes); err != nil {
		return err
	}

	return nil
}

// ServeStdio is a convenience function that creates and starts a StdioServer with os.Stdin and os.Stdout.
// It sets up signal handling for graceful shutdown on SIGTERM and SIGINT.
// Returns an error if the server encounters any issues during operation.
func ServeStdio(server *MCPServer, opts ...StdioOption) error {
	s := NewStdioServer(server)

	for _, opt := range opts {
		opt(s)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigChan
		cancel()
	}()

	return s.Listen(ctx, os.Stdin, os.Stdout)
}
