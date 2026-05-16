package udslog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/bartlettc22/gopher-cni/pkg/logging"
)

const (
	UDSLogPath = "/log"
)

type UDSLogger struct {
	loggingServer       *http.Server
	serverLogger        *logging.Logger
	pluginLoggerHandler *slog.JSONHandler
}

func NewUDSLogger() *UDSLogger {
	l := &UDSLogger{
		serverLogger:        logging.New("component", "uds-server"),
		pluginLoggerHandler: slog.NewJSONHandler(os.Stdout, nil),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(UDSLogPath, l.handleLog)
	l.loggingServer = &http.Server{
		Handler: mux,
	}
	return l
}

// StartUDSLogServer starts up a UDS server which receives log reported from CNI network plugin.
func (l *UDSLogger) StartUDSLogServer(ctx context.Context, sockAddress string) *logging.Error {
	if sockAddress == "" {
		return l.serverLogger.StructuredError("invalid UDS socket address", "address", sockAddress)
	}

	l.serverLogger.Info("starting UDS server for CNI plugin logs", "socket_address", sockAddress)

	unixListener, err := l.NewListener(sockAddress)
	if err != nil {
		return l.serverLogger.StructuredError("failed to create UDS listener", "error", err)
	}

	// Channel to capture server errors
	serverErr := make(chan error, 1)

	go func() {
		if err := l.loggingServer.Serve(unixListener); err != nil {
			serverErr <- err
		}
	}()

	// Wait for either context cancellation or server error
	select {
	case err := <-serverErr:
		if err != http.ErrServerClosed {
			return l.serverLogger.StructuredError("server exited with error", "error", err)

		} else {
			l.serverLogger.Debug("server exited")
		}
	case <-ctx.Done():
		l.serverLogger.Info("received shutdown signal")
		if err := l.loggingServer.Close(); err != nil {
			return l.serverLogger.StructuredError("server closed with error", "error", err)
		} else {
			l.serverLogger.Info("server closed successfully")
		}
	}

	return nil
}

func (l *UDSLogger) handleLog(w http.ResponseWriter, req *http.Request) {
	if req.Body == nil {
		return
	}
	defer req.Body.Close()
	data, err := io.ReadAll(req.Body)
	if err != nil {
		l.serverLogger.Error("failed to read log report from cni plugin", "error", err)
		return
	}
	l.processLog(data)
}

func (l *UDSLogger) processLog(body []byte) {

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		l.serverLogger.Error("failed to unmarshal CNI plugin log", "error", err)
		return
	}

	// Extract standard fields
	msg, _ := data["msg"].(string)
	levelStr, _ := data["level"].(string)
	timeStr, _ := data["time"].(string)

	// Parse time
	t, err := time.Parse(time.RFC3339Nano, timeStr)
	if err != nil {
		t = time.Now()
	}

	// Parse level
	var level slog.Level
	if err := level.UnmarshalText([]byte(levelStr)); err != nil {
		l.serverLogger.Error("failed to parse log level, defaulting to info", "level", levelStr, "error", err)
		level = slog.LevelInfo
	}

	// Create record
	record := slog.NewRecord(t, level, msg, 0)
	record.AddAttrs(slog.Any("component", "cni-plugin"))

	// Add remaining attributes
	for k, v := range data {
		if k != "msg" && k != "level" && k != "time" {
			record.AddAttrs(slog.Any(k, v))
		}
	}

	if err := l.pluginLoggerHandler.Handle(context.Background(), record); err != nil {
		l.serverLogger.Error("failed to handle log record", "error", err)
	}
}

func (l *UDSLogger) NewListener(path string) (net.Listener, error) {
	// Remove unix socket before use.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		// Anything other than "file not found" is an error.
		return nil, fmt.Errorf("failed to remove unix://%s: %v", path, err)
	}

	// Attempt to create the folder in case it doesn't exist
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		// If we cannot create it, just warn here - we will fail later if there is a real error
		l.serverLogger.Warn("failed to create directory", "path", path, "error", err)
	}

	var err error
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on unix socket %q: %v", path, err)
	}

	// Update file permission so that CNI plugin has permission to access it
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("uds file %q doesn't exist", path)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		return nil, fmt.Errorf("failed to update %q permission", path)
	}

	return listener, nil
}
