// entrypoint.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Configuration holds application configuration from environment variables
type Configuration struct {
	XrayBin         string
	XrayConfig      string
	HealthAddr      string
	XrayMgmtSocket  string
	ConnectTimeout  time.Duration
	GracePeriod     time.Duration
	ShutdownTimeout time.Duration
	ClientUUID      string
	ClientEmail     string
}

// Default configuration values
const (
	defaultXrayBin         = "/usr/local/bin/xray"
	defaultXrayConfig      = "/etc/xray/config.json"
	defaultHealthAddr      = ":8080"
	defaultXrayMgmtSocket  = "127.0.0.1:10085"
	defaultConnectTimeout  = 2 * time.Second
	defaultGracePeriod     = 12 * time.Second
	defaultShutdownTimeout = 5 * time.Second
	defaultClientUUID      = "00000000-0000-0000-0000-000000000000"
	defaultClientEmail     = "default@example.com"
)

// loadConfiguration loads configuration from environment variables with defaults
func loadConfiguration() *Configuration {
	config := &Configuration{
		XrayBin:         getEnvOrDefault("XRAY_BIN", defaultXrayBin),
		XrayConfig:      getEnvOrDefault("XRAY_CONFIG", defaultXrayConfig),
		HealthAddr:      getEnvOrDefault("HEALTH_ADDR", defaultHealthAddr),
		XrayMgmtSocket:  getEnvOrDefault("XRAY_MGMT_SOCKET", defaultXrayMgmtSocket),
		ConnectTimeout:  parseDurationOrDefault("CONNECT_TIMEOUT", defaultConnectTimeout),
		GracePeriod:     parseDurationOrDefault("GRACE_PERIOD", defaultGracePeriod),
		ShutdownTimeout: parseDurationOrDefault("SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
		ClientUUID:      getEnvOrDefault("XRAY_CLIENT_UUID", defaultClientUUID),
		ClientEmail:     getEnvOrDefault("XRAY_CLIENT_EMAIL", defaultClientEmail),
	}

	log.Printf("Configuration loaded: XrayBin=%s, HealthAddr=%s, MgmtSocket=%s",
		config.XrayBin, config.HealthAddr, config.XrayMgmtSocket)

	return config
}

// processConfigTemplate processes the config template with environment variables
func processConfigTemplate(config *Configuration) error {
	configPath := config.XrayConfig

	// Read config file
	configData, err := os.ReadFile(configPath)
	if err != nil {
		log.Printf("Warning: Could not read config file %s: %v", configPath, err)
		return nil // Not critical, use existing config
	}

	// Replace placeholders
	processedConfig := string(configData)
	processedConfig = strings.ReplaceAll(processedConfig, "${XRAY_CLIENT_UUID}", config.ClientUUID)
	processedConfig = strings.ReplaceAll(processedConfig, "${XRAY_CLIENT_EMAIL}", config.ClientEmail)

	// Try to write processed config back, but don't fail if filesystem is read-only
	if err := os.WriteFile(configPath, []byte(processedConfig), 0600); err != nil {
		log.Printf("Warning: Could not write processed config (filesystem may be read-only): %v", err)
		// Don't return error, just log warning and continue with original config
		return nil
	}

	log.Printf("Config processed and updated at %s", configPath)
	return nil
}

// getEnvOrDefault gets environment variable value or returns default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// parseDurationOrDefault parses duration from environment or returns default
func parseDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
		log.Printf("Warning: invalid duration format for %s, using default", key)
	}
	return defaultValue
}

// APIResponse represents a standard API response
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// RateLimiter implements a simple rate limiter
type RateLimiter struct {
	requests map[string][]time.Time
	mutex    sync.RWMutex
	limit    int
	window   time.Duration
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

// Allow checks if a request from the given IP is allowed
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	windowStart := now.Add(-rl.window)

	// Clean old requests
	if requests, exists := rl.requests[ip]; exists {
		var validRequests []time.Time
		for _, reqTime := range requests {
			if reqTime.After(windowStart) {
				validRequests = append(validRequests, reqTime)
			}
		}
		rl.requests[ip] = validRequests
	}

	// Check if limit exceeded
	if len(rl.requests[ip]) >= rl.limit {
		return false
	}

	// Add current request
	rl.requests[ip] = append(rl.requests[ip], now)
	return true
}

// getClientIP extracts the real client IP from the request
func getClientIP(r *http.Request) string {
	// Check for forwarded headers first
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}

	// Fall back to remote address
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}

// HealthChecker handles health check operations
type HealthChecker struct {
	mgmtSocket string
	timeout    time.Duration
}

// NewHealthChecker creates a new health checker instance
func NewHealthChecker(mgmtSocket string, timeout time.Duration) *HealthChecker {
	return &HealthChecker{
		mgmtSocket: mgmtSocket,
		timeout:    timeout,
	}
}

// Check performs a health check by connecting to the Xray management socket
func (h *HealthChecker) Check() error {
	d := net.Dialer{Timeout: h.timeout}
	conn, err := d.Dial("tcp", h.mgmtSocket)
	if err != nil {
		return err
	}
	return conn.Close()
}

// ProcessManager handles Xray process lifecycle
type ProcessManager struct {
	cmd         *exec.Cmd
	gracePeriod time.Duration
}

// NewProcessManager creates a new process manager instance
func NewProcessManager(binPath, configPath string) *ProcessManager {
	cmd := exec.Command(binPath, "-config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	return &ProcessManager{
		cmd:         cmd,
		gracePeriod: defaultGracePeriod, // Will be updated by config
	}
}

// SetGracePeriod sets the grace period for shutdown
func (pm *ProcessManager) SetGracePeriod(period time.Duration) {
	pm.gracePeriod = period
}

// Start starts the Xray process
func (pm *ProcessManager) Start() error {
	if err := pm.cmd.Start(); err != nil {
		return err
	}
	log.Printf("started xray (pid=%d)", pm.cmd.Process.Pid)
	return nil
}

// Wait waits for the process to complete
func (pm *ProcessManager) Wait() error {
	return pm.cmd.Wait()
}

// Signal sends a signal to the process
func (pm *ProcessManager) Signal(sig os.Signal) error {
	return pm.cmd.Process.Signal(sig)
}

// Kill forcefully kills the process
func (pm *ProcessManager) Kill() error {
	return pm.cmd.Process.Kill()
}

// GracefulShutdown attempts graceful shutdown with timeout
func (pm *ProcessManager) GracefulShutdown() error {
	done := make(chan error, 1)
	go func() {
		done <- pm.Wait()
	}()

	select {
	case <-time.After(pm.gracePeriod):
		log.Printf("grace period expired; killing xray")
		return pm.Kill()
	case err := <-done:
		log.Printf("xray exited gracefully: %v", err)
		return err
	}
}

// ServerManager handles HTTP server lifecycle
type ServerManager struct {
	server *http.Server
}

// NewServerManager creates a new server manager instance
func NewServerManager(addr string, handler http.Handler) *ServerManager {
	return &ServerManager{
		server: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 30 * time.Second, // Prevent Slowloris attacks
		},
	}
}

// Start starts the HTTP server in a goroutine
func (sm *ServerManager) Start() {
	go func() {
		log.Printf("API server starting on %s", sm.server.Addr)
		if err := sm.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server: %v", err)
		}
	}()
}

// Shutdown gracefully shuts down the server
func (sm *ServerManager) Shutdown(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return sm.server.Shutdown(ctx)
}

// HealthHandler handles health check requests
type HealthHandler struct {
	healthChecker *HealthChecker
	config        *Configuration
	rateLimiter   *RateLimiter
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(hc *HealthChecker, config *Configuration) *HealthHandler {
	return &HealthHandler{
		healthChecker: hc,
		config:        config,
		rateLimiter:   NewRateLimiter(10, time.Minute), // 10 requests per minute per IP
	}
}

// ServeHTTP implements http.Handler interface
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add security headers
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")

	// Rate limiting
	clientIP := getClientIP(r)
	if !h.rateLimiter.Allow(clientIP) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	if err := h.healthChecker.Check(); err != nil {
		http.Error(w, "xray-api-unreachable", http.StatusServiceUnavailable)
		return
	}

	h.writeResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "ok",
		Data: map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// writeResponse writes a JSON response
func (h *HealthHandler) writeResponse(w http.ResponseWriter, statusCode int, response APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
		// Don't write another header as it's already been written
	}
}

func main() {
	// Load configuration from environment
	config := loadConfiguration()

	// Validate prerequisites
	if err := validatePrerequisites(config); err != nil {
		log.Fatalf("prerequisites validation failed: %v", err)
	}

	// Process config template if available (skip if read-only filesystem)
	if err := processConfigTemplate(config); err != nil {
		log.Printf("Warning: config template processing failed: %v", err)
		// Continue with original config if template processing fails
	}

	// Initialize components
	healthChecker := NewHealthChecker(config.XrayMgmtSocket, config.ConnectTimeout)
	processManager := NewProcessManager(config.XrayBin, config.XrayConfig)
	processManager.SetGracePeriod(config.GracePeriod)
	healthHandler := NewHealthHandler(healthChecker, config)
	serverManager := NewServerManager(config.HealthAddr, healthHandler)

	// Start services
	serverManager.Start()
	if err := processManager.Start(); err != nil {
		log.Fatalf("failed to start xray: %v", err)
	}

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Main event loop
	runEventLoop(sigCh, processManager, serverManager, config)
}

// validatePrerequisites checks if required files exist
func validatePrerequisites(config *Configuration) error {
	if _, err := os.Stat(config.XrayBin); err != nil {
		return fmt.Errorf("xray binary not found at %s: %v", config.XrayBin, err)
	}
	return nil
}

// runEventLoop handles the main event loop
func runEventLoop(sigCh <-chan os.Signal, pm *ProcessManager, sm *ServerManager, config *Configuration) {
	done := make(chan error, 1)
	go func() {
		done <- pm.Wait()
	}()

	for {
		select {
		case sig := <-sigCh:
			handleSignal(sig, pm, sm, config)
		case err := <-done:
			handleProcessExit(err, sm, config)
		}
	}
}

// handleSignal processes system signals
func handleSignal(sig os.Signal, pm *ProcessManager, sm *ServerManager, config *Configuration) {
	if sig == syscall.SIGHUP {
		log.Println("SIGHUP received: configuration reload not implemented")
		return
	}

	log.Printf("forwarding %v to xray process", sig)
	if err := pm.Signal(sig); err != nil {
		log.Printf("failed to signal xray: %v", err)
		return
	}

	if err := pm.GracefulShutdown(); err != nil {
		log.Printf("failed to shutdown xray gracefully: %v", err)
	}

	if err := sm.Shutdown(config.ShutdownTimeout); err != nil {
		log.Printf("failed to shutdown server: %v", err)
	}
	os.Exit(0)
}

// handleProcessExit handles process exit events
func handleProcessExit(err error, sm *ServerManager, config *Configuration) {
	if err != nil {
		log.Printf("xray process exited with error: %v", err)
		os.Exit(1)
	}

	log.Println("xray process exited cleanly")
	if err := sm.Shutdown(config.ShutdownTimeout); err != nil {
		log.Printf("failed to shutdown server: %v", err)
	}
	os.Exit(0)
}
