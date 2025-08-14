// entrypoint.go
package main

import (
	"context"
	"encoding/json"
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

const (
	xrayBin        = "/usr/local/bin/xray"
	xrayConfig     = "/etc/xray/config.json"
	healthAddr     = ":8080"
	xrayMgmtSocket = "127.0.0.1:10085"
	connectTimeout = 2 * time.Second
	gracePeriod    = 12 * time.Second
	forceKillAfter = 5 * time.Second
)

// User represents a VLESS user
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Level int    `json:"level"`
	Flow  string `json:"flow"`
}

// TrafficStats represents user traffic statistics
type TrafficStats struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	Uplink   int64  `json:"uplink"`
	Downlink int64  `json:"downlink"`
	Total    int64  `json:"total"`
	LastSeen string `json:"last_seen"`
}

// Quota represents user quota limits
type Quota struct {
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	DailyLimit   int64  `json:"daily_limit"`   // in bytes
	MonthlyLimit int64  `json:"monthly_limit"` // in bytes
	UsedToday    int64  `json:"used_today"`
	UsedMonth    int64  `json:"used_month"`
	ResetTime    string `json:"reset_time"`
}

// APIResponse represents a standard API response
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// Global state for user management
var (
	users    = make(map[string]*User)
	quotas   = make(map[string]*Quota)
	stats    = make(map[string]*TrafficStats)
	statsMux sync.RWMutex
	quotaMux sync.RWMutex
)

func main() {
	// Ensure xray binary exists
	if _, err := os.Stat(xrayBin); err != nil {
		log.Fatalf("xray binary missing: %v", err)
	}

	// Initialize default users from config
	initializeUsers()

	// Start API server
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("/", healthHandler)

	// User management endpoints
	mux.HandleFunc("/api/users", usersHandler)
	mux.HandleFunc("/api/users/", userHandler)

	// Traffic statistics endpoints
	mux.HandleFunc("/api/stats", statsHandler)
	mux.HandleFunc("/api/stats/", userStatsHandler)

	// Quota management endpoints
	mux.HandleFunc("/api/quotas", quotasHandler)
	mux.HandleFunc("/api/quotas/", quotaHandler)

	// System endpoints
	mux.HandleFunc("/api/system", systemHandler)
	mux.HandleFunc("/api/reload", reloadHandler)

	srv := &http.Server{Addr: healthAddr, Handler: mux}
	go func() {
		log.Printf("API server starting on %s", healthAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server: %v", err)
		}
	}()

	// Start background tasks
	go statsCollector()
	go quotaEnforcer()

	// Build xray command
	cmd := exec.Command(xrayBin, "-config", xrayConfig)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	// Start xray
	if err := cmd.Start(); err != nil {
		log.Fatalf("failed to start xray: %v", err)
	}
	log.Printf("started xray (pid=%d)", cmd.Process.Pid)

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	for {
		select {
		case sig := <-sigCh:
			log.Printf("received signal: %v", sig)
			switch sig {
			case syscall.SIGHUP:
				log.Println("SIGHUP received: reloading configuration")
				reloadConfiguration()
			default:
				log.Printf("forwarding %v to xray process", sig)
				_ = cmd.Process.Signal(sig)
				// Wait gracePeriod for graceful stop
				select {
				case <-time.After(gracePeriod):
					log.Printf("grace period expired; killing xray")
					_ = cmd.Process.Kill()
				case err := <-done:
					log.Printf("xray exited gracefully: %v", err)
				}
				// Shutdown API server
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = srv.Shutdown(ctx)
				cancel()
				os.Exit(0)
			}
		case err := <-done:
			if err != nil {
				log.Printf("xray process exited with error: %v", err)
				os.Exit(1)
			}
			log.Println("xray process exited cleanly")
			// Shutdown API server and exit
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = srv.Shutdown(ctx)
			cancel()
			os.Exit(0)
		}
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Fast TCP connect to the mgmt API (local only)
	d := net.Dialer{Timeout: connectTimeout}
	conn, err := d.Dial("tcp", xrayMgmtSocket)
	if err != nil {
		http.Error(w, "xray-api-unreachable", http.StatusServiceUnavailable)
		return
	}
	_ = conn.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := APIResponse{
		Success: true,
		Message: "ok",
		Data: map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"uptime":    getUptime(),
		},
	}
	json.NewEncoder(w).Encode(response)
}

func usersHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "GET":
		statsMux.RLock()
		userList := make([]*User, 0, len(users))
		for _, user := range users {
			userList = append(userList, user)
		}
		statsMux.RUnlock()

		response := APIResponse{
			Success: true,
			Data:    userList,
		}
		json.NewEncoder(w).Encode(response)

	case "POST":
		var user User
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if user.ID == "" {
			http.Error(w, "User ID is required", http.StatusBadRequest)
			return
		}

		statsMux.Lock()
		users[user.ID] = &user
		statsMux.Unlock()

		// Initialize quota for new user
		quotaMux.Lock()
		quotas[user.ID] = &Quota{
			UserID:       user.ID,
			Email:        user.Email,
			DailyLimit:   10 * 1024 * 1024 * 1024,  // 10GB default
			MonthlyLimit: 100 * 1024 * 1024 * 1024, // 100GB default
			ResetTime:    time.Now().UTC().Format(time.RFC3339),
		}
		quotaMux.Unlock()

		response := APIResponse{
			Success: true,
			Message: "User created successfully",
			Data:    user,
		}
		json.NewEncoder(w).Encode(response)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func userHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}
	userID := parts[3]

	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "GET":
		statsMux.RLock()
		user, exists := users[userID]
		statsMux.RUnlock()

		if !exists {
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}

		response := APIResponse{
			Success: true,
			Data:    user,
		}
		json.NewEncoder(w).Encode(response)

	case "PUT":
		var user User
		if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		statsMux.Lock()
		if _, exists := users[userID]; !exists {
			statsMux.Unlock()
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		user.ID = userID // Ensure ID matches
		users[userID] = &user
		statsMux.Unlock()

		response := APIResponse{
			Success: true,
			Message: "User updated successfully",
			Data:    user,
		}
		json.NewEncoder(w).Encode(response)

	case "DELETE":
		statsMux.Lock()
		if _, exists := users[userID]; !exists {
			statsMux.Unlock()
			http.Error(w, "User not found", http.StatusNotFound)
			return
		}
		delete(users, userID)
		statsMux.Unlock()

		quotaMux.Lock()
		delete(quotas, userID)
		quotaMux.Unlock()

		response := APIResponse{
			Success: true,
			Message: "User deleted successfully",
		}
		json.NewEncoder(w).Encode(response)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	statsMux.RLock()
	statsList := make([]*TrafficStats, 0, len(stats))
	for _, stat := range stats {
		statsList = append(statsList, stat)
	}
	statsMux.RUnlock()

	response := APIResponse{
		Success: true,
		Data:    statsList,
	}
	json.NewEncoder(w).Encode(response)
}

func userStatsHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}
	userID := parts[3]

	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	statsMux.RLock()
	stat, exists := stats[userID]
	statsMux.RUnlock()

	if !exists {
		http.Error(w, "Stats not found", http.StatusNotFound)
		return
	}

	response := APIResponse{
		Success: true,
		Data:    stat,
	}
	json.NewEncoder(w).Encode(response)
}

func quotasHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "GET":
		quotaMux.RLock()
		quotaList := make([]*Quota, 0, len(quotas))
		for _, quota := range quotas {
			quotaList = append(quotaList, quota)
		}
		quotaMux.RUnlock()

		response := APIResponse{
			Success: true,
			Data:    quotaList,
		}
		json.NewEncoder(w).Encode(response)

	case "POST":
		var quota Quota
		if err := json.NewDecoder(r.Body).Decode(&quota); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if quota.UserID == "" {
			http.Error(w, "User ID is required", http.StatusBadRequest)
			return
		}

		quotaMux.Lock()
		quotas[quota.UserID] = &quota
		quotaMux.Unlock()

		response := APIResponse{
			Success: true,
			Message: "Quota created successfully",
			Data:    quota,
		}
		json.NewEncoder(w).Encode(response)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func quotaHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}
	userID := parts[3]

	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "GET":
		quotaMux.RLock()
		quota, exists := quotas[userID]
		quotaMux.RUnlock()

		if !exists {
			http.Error(w, "Quota not found", http.StatusNotFound)
			return
		}

		response := APIResponse{
			Success: true,
			Data:    quota,
		}
		json.NewEncoder(w).Encode(response)

	case "PUT":
		var quota Quota
		if err := json.NewDecoder(r.Body).Decode(&quota); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		quotaMux.Lock()
		if _, exists := quotas[userID]; !exists {
			quotaMux.Unlock()
			http.Error(w, "Quota not found", http.StatusNotFound)
			return
		}
		quota.UserID = userID // Ensure ID matches
		quotas[userID] = &quota
		quotaMux.Unlock()

		response := APIResponse{
			Success: true,
			Message: "Quota updated successfully",
			Data:    quota,
		}
		json.NewEncoder(w).Encode(response)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func systemHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	systemInfo := map[string]interface{}{
		"uptime":        getUptime(),
		"users":         len(users),
		"active_users":  getActiveUsers(),
		"total_traffic": getTotalTraffic(),
		"memory_usage":  getMemoryUsage(),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}

	response := APIResponse{
		Success: true,
		Data:    systemInfo,
	}
	json.NewEncoder(w).Encode(response)
}

func reloadHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	go reloadConfiguration()

	response := APIResponse{
		Success: true,
		Message: "Configuration reload initiated",
	}
	json.NewEncoder(w).Encode(response)
}

// Helper functions

func initializeUsers() {
	// Initialize with default user from config
	defaultUser := &User{
		ID:    "a6536f0d-5663-4906-b75d-1861775782b1",
		Email: "test@example.com",
		Level: 0,
		Flow:  "xtls-rprx-vision",
	}
	users[defaultUser.ID] = defaultUser

	// Initialize quota for default user
	quotas[defaultUser.ID] = &Quota{
		UserID:       defaultUser.ID,
		Email:        defaultUser.Email,
		DailyLimit:   10 * 1024 * 1024 * 1024,  // 10GB
		MonthlyLimit: 100 * 1024 * 1024 * 1024, // 100GB
		ResetTime:    time.Now().UTC().Format(time.RFC3339),
	}

	log.Printf("Initialized %d users", len(users))
}

func statsCollector() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			collectStats()
		}
	}
}

func collectStats() {
	// Simulate collecting stats from Xray API
	// In a real implementation, this would query the Xray API
	statsMux.Lock()
	defer statsMux.Unlock()

	for userID, user := range users {
		if _, exists := stats[userID]; !exists {
			stats[userID] = &TrafficStats{
				UserID:   userID,
				Email:    user.Email,
				LastSeen: time.Now().UTC().Format(time.RFC3339),
			}
		}

		// Simulate traffic data (in real implementation, get from Xray)
		stats[userID].Uplink += int64(time.Now().Unix() % 1000)
		stats[userID].Downlink += int64(time.Now().Unix() % 2000)
		stats[userID].Total = stats[userID].Uplink + stats[userID].Downlink
		stats[userID].LastSeen = time.Now().UTC().Format(time.RFC3339)
	}
}

func quotaEnforcer() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			enforceQuotas()
		}
	}
}

func enforceQuotas() {
	quotaMux.Lock()
	defer quotaMux.Unlock()

	now := time.Now()

	for userID, quota := range quotas {
		// Check daily quota
		if quota.UsedToday >= quota.DailyLimit {
			log.Printf("User %s has exceeded daily quota", userID)
			// In a real implementation, this would disable the user
		}

		// Check monthly quota
		if quota.UsedMonth >= quota.MonthlyLimit {
			log.Printf("User %s has exceeded monthly quota", userID)
			// In a real implementation, this would disable the user
		}

		// Reset daily quota at midnight
		resetTime, _ := time.Parse(time.RFC3339, quota.ResetTime)
		if now.Sub(resetTime) >= 24*time.Hour {
			quota.UsedToday = 0
			quota.ResetTime = now.UTC().Format(time.RFC3339)
		}
	}
}

func reloadConfiguration() {
	log.Println("Reloading Xray configuration...")
	// In a real implementation, this would reload the Xray config
	// For now, just log the action
}

func getUptime() string {
	// In a real implementation, this would calculate actual uptime
	return "1h 23m 45s"
}

func getActiveUsers() int {
	statsMux.RLock()
	defer statsMux.RUnlock()

	active := 0
	now := time.Now()

	for _, stat := range stats {
		lastSeen, err := time.Parse(time.RFC3339, stat.LastSeen)
		if err == nil && now.Sub(lastSeen) < 5*time.Minute {
			active++
		}
	}

	return active
}

func getTotalTraffic() int64 {
	statsMux.RLock()
	defer statsMux.RUnlock()

	total := int64(0)
	for _, stat := range stats {
		total += stat.Total
	}

	return total
}

func getMemoryUsage() map[string]interface{} {
	// In a real implementation, this would get actual memory usage
	return map[string]interface{}{
		"used":       "128MB",
		"total":      "512MB",
		"percentage": 25,
	}
}
