package webhook

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/bartlettc22/gopher-cni/internal/logging"
)

// Server represents the webhook server
type Server struct {
	Config     *WebhookConfig
	httpServer *http.Server
	log        *logging.Logger
}

// NewServer creates a new webhook server
func NewServer(config *WebhookConfig) *Server {
	log := logging.New("component", "webhook-server")
	return &Server{
		Config: config,
		log:    log,
	}
}

// Run starts the webhook server
func (s *Server) Run(ctx context.Context) *logging.Error {

	s.log.Info("starting webhook server", "port", s.Config.Port)

	s.log.Info("loading TLS configuration",
		"tls_cert", s.Config.TLSCertPath,
		"tls_key", s.Config.TLSKeyPath,
	)
	tlsConfig, err := s.loadTLSConfig()
	if err != nil {
		return s.log.StructuredError("failed to load TLS config", "error", err)
	}

	// Create HTTP server mux
	mux := http.NewServeMux()

	// Register handlers
	mutateHandler := NewMutateHandler(s.Config)
	validateHandler := NewValidateHandler(s.Config)

	mux.Handle("/mutate", mutateHandler)
	mux.Handle("/validate", validateHandler)
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/ready", s.readyHandler)

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:      fmt.Sprintf(":%d", s.Config.Port),
		TLSConfig: tlsConfig,
		Handler:   mux,
		// Security settings
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Channel to listen for errors coming from the listener
	serverErrors := make(chan error, 1)

	// Start the server
	go func() {
		s.log.Info("webhook server listening", "port", s.Config.Port, "protocol", "HTTPS")
		serverErrors <- s.httpServer.ListenAndServeTLS("", "")
	}()

	// Block until we receive a signal or an error
	select {
	case err := <-serverErrors:
		return s.log.StructuredError("server error", "error", err)

	case <-ctx.Done():
		s.log.Info("received shutdown signal")

		// Attempt graceful shutdown
		if err := s.httpServer.Shutdown(context.Background()); err != nil {
			return s.log.StructuredError("failed to gracefully shutdown", "error", err)
		}

		s.log.Info("server stopped gracefully")
	}

	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// loadTLSConfig loads the TLS configuration
func (s *Server) loadTLSConfig() (*tls.Config, error) {
	// Load certificate and key
	cert, err := tls.LoadX509KeyPair(s.Config.TLSCertPath, s.Config.TLSKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		},
	}, nil
}

// healthHandler handles health check requests
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok")); err != nil {
		s.log.Error("failed to write health check response", "error", err)
	}
}

// readyHandler handles readiness check requests
func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
	// Add any readiness checks here (e.g., checking dependencies)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ready")); err != nil {
		s.log.Error("failed to write readiness check response", "error", err)
	}
}
