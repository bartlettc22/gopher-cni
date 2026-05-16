package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/bartlettc22/gopher-cni/pkg/cni"
	"github.com/bartlettc22/gopher-cni/pkg/udslog"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/google/uuid"
)


// UDSLogWriter implements io.Writer and sends logs to a UDS server via HTTP
type UDSLogWriter struct {
	client   *http.Client
	endpoint string
	mu       sync.Mutex
}

// NewUDSLogWriter creates a new writer that sends logs to a Unix Domain Socket server
func NewUDSLogWriter(socketPath string) *UDSLogWriter {
	// Create HTTP client configured to use Unix Domain Socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
		Timeout: 2 * time.Second,
	}

	return &UDSLogWriter{
		client: client,

		// The scheme and host don't matter for UDS
		endpoint: "http://unix" + udslog.UDSLogPath,
	}
}

// Write implements io.Writer interface
func (w *UDSLogWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Send the log entry to the UDS server
	resp, err := w.client.Post(w.endpoint, "application/json", bytes.NewReader(p))
	if err != nil {
		return 0, fmt.Errorf("failed to send log to UDS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf("UDS server returned status: %d", resp.StatusCode)
	}

	return len(p), nil
}

func newLogger(conf *cni.PluginConfig, args *skel.CmdArgs, funcName string) (*slog.Logger, error) {

	var base *slog.Logger

	logLevel := slog.LevelInfo
	if conf.LogLevel != "" {
		switch conf.LogLevel {
		case "debug":
			logLevel = slog.LevelDebug
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}
	}

	if conf.LogUDSAddress != "" {
		udsWriter := NewUDSLogWriter(conf.LogUDSAddress)
		base = slog.New(slog.NewJSONHandler(udsWriter, &slog.HandlerOptions{
			Level: logLevel,
		}))
	} else {
		// Send nowhere if UDS logging is disabled
		base = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return base.With(
		"eventID", uuid.New(),
		"containerID", args.ContainerID,
		"netNSName", args.Netns,
		"interface", args.IfName,
		"args", args.Args,
		"path", args.Path,
		"cmd", funcName,
	), nil
}
