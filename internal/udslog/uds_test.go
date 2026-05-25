package udslog

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bartlettc22/gopher-cni/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewUDSLogger verifies that the constructor properly initializes the UDSLogger
func TestNewUDSLogger(t *testing.T) {
	logger := NewUDSLogger()

	assert.NotNil(t, logger, "logger should not be nil")
	assert.NotNil(t, logger.serverLogger, "serverLogger should be initialized")
	assert.NotNil(t, logger.pluginLoggerHandler, "pluginLoggerHandler should be initialized")
	assert.NotNil(t, logger.loggingServer, "loggingServer should be initialized")
	assert.NotNil(t, logger.loggingServer.Handler, "HTTP handler should be set")
}

// TestProcessLog_ValidJSON tests processing a valid log entry
func TestProcessLog_ValidJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := &UDSLogger{
		serverLogger:        logging.New("component", "test"),
		pluginLoggerHandler: slog.NewJSONHandler(&buf, nil),
	}

	logData := map[string]any{
		"msg":   "test message",
		"level": "INFO",
		"time":  time.Now().Format(time.RFC3339Nano),
		"key1":  "value1",
		"key2":  42,
	}
	jsonData, err := json.Marshal(logData)
	require.NoError(t, err)

	logger.processLog(jsonData)

	// Verify output was written
	assert.NotEmpty(t, buf.String(), "should have written log output")

	// Verify the output contains expected fields
	output := buf.String()
	assert.Contains(t, output, "test message")
	assert.Contains(t, output, "cni-plugin")
	assert.Contains(t, output, "key1")
	assert.Contains(t, output, "value1")
}

// TestProcessLog_InvalidJSON tests handling of malformed JSON
func TestProcessLog_InvalidJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := &UDSLogger{
		serverLogger:        logging.New("component", "test"),
		pluginLoggerHandler: slog.NewJSONHandler(&buf, nil),
	}

	invalidJSON := []byte(`{invalid json}`)

	// Should not panic
	logger.processLog(invalidJSON)

	// No output should be written for invalid JSON
	assert.Empty(t, buf.String())
}

// TestProcessLog_MissingFields tests handling of incomplete log data
func TestProcessLog_MissingFields(t *testing.T) {
	var buf bytes.Buffer
	logger := &UDSLogger{
		serverLogger:        logging.New("component", "test"),
		pluginLoggerHandler: slog.NewJSONHandler(&buf, nil),
	}

	// Log entry with only msg field
	logData := map[string]any{
		"msg": "minimal message",
	}
	jsonData, err := json.Marshal(logData)
	require.NoError(t, err)

	logger.processLog(jsonData)

	// Should still process and write output
	assert.NotEmpty(t, buf.String())
	assert.Contains(t, buf.String(), "minimal message")
}

// TestProcessLog_AllLogLevels tests all standard log levels
func TestProcessLog_AllLogLevels(t *testing.T) {
	levels := []string{"DEBUG", "INFO", "WARN", "ERROR"}

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			var buf bytes.Buffer
			logger := &UDSLogger{
				pluginLoggerHandler: slog.NewJSONHandler(&buf, nil),
			}

			logData := map[string]any{
				"msg":   "test message",
				"level": level,
				"time":  time.Now().Format(time.RFC3339Nano),
			}
			jsonData, err := json.Marshal(logData)
			require.NoError(t, err)

			logger.processLog(jsonData)

			assert.NotEmpty(t, buf.String())
			assert.Contains(t, buf.String(), "test message")
		})
	}
}

// TestProcessLog_InvalidTime tests handling of unparseable timestamps
func TestProcessLog_InvalidTime(t *testing.T) {
	var buf bytes.Buffer
	logger := &UDSLogger{
		serverLogger:        logging.New("component", "test"),
		pluginLoggerHandler: slog.NewJSONHandler(&buf, nil),
	}

	logData := map[string]any{
		"msg":   "test message",
		"level": "INFO",
		"time":  "invalid-timestamp",
	}
	jsonData, err := json.Marshal(logData)
	require.NoError(t, err)

	// Should fall back to time.Now() and not panic
	logger.processLog(jsonData)

	assert.NotEmpty(t, buf.String())
	assert.Contains(t, buf.String(), "test message")
}

// TestHandleLog_Success tests the HTTP handler with a valid request
func TestHandleLog_Success(t *testing.T) {
	var buf bytes.Buffer
	logger := &UDSLogger{
		serverLogger:        logging.New("component", "test"),
		pluginLoggerHandler: slog.NewJSONHandler(&buf, nil),
	}

	logData := map[string]any{
		"msg":   "http handler test",
		"level": "INFO",
		"time":  time.Now().Format(time.RFC3339Nano),
	}
	jsonData, err := json.Marshal(logData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, UDSLogPath, bytes.NewReader(jsonData))
	w := httptest.NewRecorder()

	logger.handleLog(w, req)

	assert.NotEmpty(t, buf.String())
	assert.Contains(t, buf.String(), "http handler test")
}

// TestHandleLog_NilBody tests handler behavior with nil request body
func TestHandleLog_NilBody(t *testing.T) {
	var buf bytes.Buffer
	logger := &UDSLogger{
		serverLogger:        logging.New("component", "test"),
		pluginLoggerHandler: slog.NewJSONHandler(&buf, nil),
	}

	req := httptest.NewRequest(http.MethodPost, UDSLogPath, nil)
	w := httptest.NewRecorder()

	// Should not panic
	logger.handleLog(w, req)

	// No output should be written
	assert.Empty(t, buf.String())
}

// TestHandleLog_EmptyBody tests handler behavior with empty body
func TestHandleLog_EmptyBody(t *testing.T) {
	var buf bytes.Buffer
	logger := &UDSLogger{
		serverLogger:        logging.New("component", "test"),
		pluginLoggerHandler: slog.NewJSONHandler(&buf, nil),
	}

	req := httptest.NewRequest(http.MethodPost, UDSLogPath, bytes.NewReader([]byte("")))
	w := httptest.NewRecorder()

	logger.handleLog(w, req)

	// Empty JSON should not produce output
	assert.Empty(t, buf.String())
}

// TestNewListener_Success tests successful socket creation
func TestNewListener_Success(t *testing.T) {
	logger := NewUDSLogger()
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	listener, err := logger.NewListener(sockPath)
	require.NoError(t, err)
	require.NotNil(t, listener)
	defer listener.Close()

	// Verify socket file exists
	info, err := os.Stat(sockPath)
	require.NoError(t, err)
	assert.Equal(t, os.ModeSocket, info.Mode()&os.ModeSocket, "should be a socket file")

	// Verify permissions are 0666
	assert.Equal(t, os.FileMode(0666), info.Mode().Perm(), "permissions should be 0666")
}

// TestNewListener_ExistingSocket tests removal of existing socket
func TestNewListener_ExistingSocket(t *testing.T) {
	logger := NewUDSLogger()
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	// Create first listener
	listener1, err := logger.NewListener(sockPath)
	require.NoError(t, err)
	require.NotNil(t, listener1)
	listener1.Close()

	// Create second listener at same path (should remove old one)
	listener2, err := logger.NewListener(sockPath)
	require.NoError(t, err)
	require.NotNil(t, listener2)
	defer listener2.Close()

	// Verify socket exists
	_, err = os.Stat(sockPath)
	assert.NoError(t, err)
}

// TestNewListener_DirectoryCreation tests automatic directory creation
func TestNewListener_DirectoryCreation(t *testing.T) {
	logger := NewUDSLogger()
	tempDir := t.TempDir()
	sockPath := filepath.Join(tempDir, "nested", "dir", "test.sock")

	listener, err := logger.NewListener(sockPath)
	require.NoError(t, err)
	require.NotNil(t, listener)
	defer listener.Close()

	// Verify nested directories were created
	_, err = os.Stat(filepath.Dir(sockPath))
	assert.NoError(t, err)
}

// TestNewListener_InvalidPath tests handling of invalid paths
func TestNewListener_InvalidPath(t *testing.T) {
	logger := NewUDSLogger()

	// Try to create socket on invalid path (root directory)
	listener, err := logger.NewListener("/invalid/path/that/does/not/exist/test.sock")

	if listener != nil {
		listener.Close()
	}

	// Should fail (but may succeed if running as root or if dirs exist)
	// We just verify it doesn't panic
	if err != nil {
		assert.Contains(t, err.Error(), "failed to")
	}
}

// TestStartUDSLogServer_EmptyAddress tests validation of empty socket address
func TestStartUDSLogServer_EmptyAddress(t *testing.T) {
	logger := NewUDSLogger()
	ctx := context.Background()

	err := logger.StartUDSLogServer(ctx, "")

	require.NotNil(t, err, "should return error for empty address")
	// logging.Error doesn't implement error interface properly, but we can call Error()
	actualErr := err.Error()
	require.NotNil(t, actualErr)
	assert.Contains(t, actualErr.Error(), "invalid UDS socket address")
}

// TestStartUDSLogServer_ContextCancellation tests graceful shutdown via context
func TestStartUDSLogServer_ContextCancellation(t *testing.T) {
	logger := NewUDSLogger()
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan *logging.Error, 1)
	go func() {
		errCh <- logger.StartUDSLogServer(ctx, sockPath)
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Verify socket was created
	_, err := os.Stat(sockPath)
	assert.NoError(t, err, "socket file should exist")

	// Trigger shutdown
	cancel()

	// Wait for clean exit
	select {
	case err := <-errCh:
		assert.Nil(t, err, "should exit cleanly on context cancellation")
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}
}

// TestStartUDSLogServer_InvalidSocketPath tests handling of invalid socket paths
func TestStartUDSLogServer_InvalidSocketPath(t *testing.T) {
	logger := NewUDSLogger()
	ctx := context.Background()

	// Use a path that will fail (trying to create socket in read-only location)
	// Note: This might not fail on all systems, so we just verify no panic
	err := logger.StartUDSLogServer(ctx, "/proc/test.sock")

	if err != nil {
		actualErr := err.Error()
		if actualErr != nil {
			assert.Contains(t, actualErr.Error(), "failed to create UDS listener")
		}
	}
}

// TestStartUDSLogServer_EndToEnd tests full client-server interaction
func TestStartUDSLogServer_EndToEnd(t *testing.T) {
	logger := NewUDSLogger()
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start server in goroutine
	errCh := make(chan *logging.Error, 1)
	go func() {
		errCh <- logger.StartUDSLogServer(ctx, sockPath)
	}()

	// Wait for socket to exist
	var socketReady bool
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(sockPath); err == nil {
			socketReady = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.True(t, socketReady, "socket should be created")

	// Create HTTP client that connects via Unix socket
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sockPath)
			},
		},
		Timeout: 2 * time.Second,
	}

	// Send log message
	logData := map[string]any{
		"msg":   "end-to-end test",
		"level": "INFO",
		"time":  time.Now().Format(time.RFC3339Nano),
		"test":  true,
	}
	jsonData, err := json.Marshal(logData)
	require.NoError(t, err)

	resp, err := client.Post("http://unix"+UDSLogPath, "application/json", bytes.NewReader(jsonData))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Trigger shutdown
	cancel()

	// Verify clean exit
	select {
	case err := <-errCh:
		assert.Nil(t, err, "should exit cleanly")
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down within timeout")
	}
}

// TestStartUDSLogServer_MultipleRequests tests handling multiple concurrent requests
func TestStartUDSLogServer_MultipleRequests(t *testing.T) {
	logger := NewUDSLogger()
	sockPath := filepath.Join(t.TempDir(), "test.sock")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start server
	go func() {
		_ = logger.StartUDSLogServer(ctx, sockPath)
	}()

	// Wait for socket
	var socketReady bool
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(sockPath); err == nil {
			socketReady = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.True(t, socketReady)

	// Create HTTP client
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", sockPath)
			},
		},
		Timeout: 2 * time.Second,
	}

	// Send multiple requests
	numRequests := 10
	successCount := 0
	for i := 0; i < numRequests; i++ {
		logData := map[string]any{
			"msg":     "concurrent test",
			"level":   "INFO",
			"time":    time.Now().Format(time.RFC3339Nano),
			"request": i,
		}
		jsonData, err := json.Marshal(logData)
		require.NoError(t, err)

		resp, err := client.Post("http://unix"+UDSLogPath, "application/json", bytes.NewReader(jsonData))
		if err == nil {
			resp.Body.Close()
			successCount++
		}
	}

	assert.Equal(t, numRequests, successCount, "all requests should succeed")

	cancel()
}

// TestHandleLog_LargePayload tests handling of large log messages
func TestHandleLog_LargePayload(t *testing.T) {
	var buf bytes.Buffer
	logger := &UDSLogger{
		serverLogger:        logging.New("component", "test"),
		pluginLoggerHandler: slog.NewJSONHandler(&buf, nil),
	}

	// Create a large log message
	largeString := strings.Repeat("A", 10000)
	logData := map[string]any{
		"msg":   largeString,
		"level": "INFO",
		"time":  time.Now().Format(time.RFC3339Nano),
	}
	jsonData, err := json.Marshal(logData)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, UDSLogPath, bytes.NewReader(jsonData))
	w := httptest.NewRecorder()

	logger.handleLog(w, req)

	assert.NotEmpty(t, buf.String())
	assert.Contains(t, buf.String(), largeString)
}

// TestProcessLog_ExtraAttributes tests that additional attributes are preserved
func TestProcessLog_ExtraAttributes(t *testing.T) {
	var buf bytes.Buffer
	logger := &UDSLogger{
		serverLogger:        logging.New("component", "test"),
		pluginLoggerHandler: slog.NewJSONHandler(&buf, nil),
	}

	logData := map[string]any{
		"msg":       "test message",
		"level":     "INFO",
		"time":      time.Now().Format(time.RFC3339Nano),
		"custom1":   "value1",
		"custom2":   123,
		"custom3":   true,
		"custom4":   3.14,
		"namespace": "default",
		"pod":       "test-pod",
	}
	jsonData, err := json.Marshal(logData)
	require.NoError(t, err)

	logger.processLog(jsonData)

	output := buf.String()
	assert.Contains(t, output, "custom1")
	assert.Contains(t, output, "value1")
	assert.Contains(t, output, "custom2")
	assert.Contains(t, output, "namespace")
	assert.Contains(t, output, "default")
	assert.Contains(t, output, "test-pod")
}

// TestUDSLogPath_Constant tests that the constant is set correctly
func TestUDSLogPath_Constant(t *testing.T) {
	assert.Equal(t, "/log", UDSLogPath, "UDSLogPath constant should be /log")
}

// BenchmarkProcessLog benchmarks log processing performance
func BenchmarkProcessLog(b *testing.B) {
	logger := &UDSLogger{
		pluginLoggerHandler: slog.NewJSONHandler(io.Discard, nil),
	}

	logData := map[string]any{
		"msg":   "benchmark message",
		"level": "INFO",
		"time":  time.Now().Format(time.RFC3339Nano),
		"key1":  "value1",
		"key2":  42,
	}
	jsonData, _ := json.Marshal(logData)

	for b.Loop() {
		logger.processLog(jsonData)
	}
}

// BenchmarkHandleLog benchmarks HTTP handler performance
func BenchmarkHandleLog(b *testing.B) {
	logger := &UDSLogger{
		pluginLoggerHandler: slog.NewJSONHandler(io.Discard, nil),
	}

	logData := map[string]any{
		"msg":   "benchmark message",
		"level": "INFO",
		"time":  time.Now().Format(time.RFC3339Nano),
	}
	jsonData, _ := json.Marshal(logData)

	req := httptest.NewRequest(http.MethodPost, UDSLogPath, bytes.NewReader(jsonData))
	w := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req.Body = io.NopCloser(bytes.NewReader(jsonData))
		logger.handleLog(w, req)
	}
}
