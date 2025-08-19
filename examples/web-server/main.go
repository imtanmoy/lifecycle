package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/imtanmoy/lifecycle"
)

// Server stats for monitoring
type Stats struct {
	StartTime    time.Time `json:"start_time"`
	Requests     int64     `json:"total_requests"`
	ActiveConns  int64     `json:"active_connections"`
	HealthChecks int64     `json:"health_checks"`
}

var serverStats = &Stats{
	StartTime: time.Now(),
}

func main() {
	// Create HTTP server with routes
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", healthHandler)

	// API endpoints
	mux.HandleFunc("/api/status", statusHandler)
	mux.HandleFunc("/api/users", usersHandler)

	// Add request counting middleware
	handler := requestCounterMiddleware(loggingMiddleware(mux))

	server := &http.Server{
		Addr:         ":8080",
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Create lifecycle with comprehensive setup
	lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
		opts.ShutdownTimeout = 30 * time.Second

		// Pre-start: Validate configuration
		hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
			log.Println("🔍 Validating server configuration...")

			// Check if port is available
			if server.Addr == "" {
				return fmt.Errorf("server address not configured")
			}

			// Validate environment
			if os.Getenv("ENV") == "" {
				log.Println("⚠️  ENV not set, defaulting to 'development'")
				os.Setenv("ENV", "development")
			}

			log.Printf("✅ Configuration valid - listening on %s", server.Addr)
			return nil
		})

		// Start: Initialize services
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("🚀 Starting web server...")
			serverStats.StartTime = time.Now()
			return nil
		})

		// Signal: Prepare for shutdown
		hooks.OnSignal = append(hooks.OnSignal, func(ctx context.Context) error {
			log.Println("📡 Shutdown signal received, preparing to stop...")

			// Stop accepting new connections
			log.Printf("📊 Final stats - Requests: %d, Active: %d",
				atomic.LoadInt64(&serverStats.Requests),
				atomic.LoadInt64(&serverStats.ActiveConns))

			return nil
		})

		// Shutdown: Graceful server shutdown
		hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
			log.Println("🛑 Shutting down HTTP server gracefully...")

			// The HTTP server shutdown is handled by AttachHTTPServer
			// This hook can be used for additional shutdown logic
			return nil
		})

		// Exit: Final cleanup
		hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
			uptime := time.Since(serverStats.StartTime)
			totalReqs := atomic.LoadInt64(&serverStats.Requests)

			log.Printf("📈 Server Statistics:")
			log.Printf("   Uptime: %v", uptime)
			log.Printf("   Total Requests: %d", totalReqs)
			log.Printf("   Avg Req/sec: %.2f", float64(totalReqs)/uptime.Seconds())
			log.Println("✨ Server shutdown complete!")

			return nil
		})
	}).AttachHTTPServer(server)

	// Start the server
	log.Println("🌟 Web Server Example")
	log.Printf("🌐 Server starting on http://localhost%s", server.Addr)
	log.Println("📋 Available endpoints:")
	log.Println("   GET /health - Health check")
	log.Println("   GET /api/status - Server status")
	log.Println("   GET /api/users - Mock users API")
	log.Println("💡 Press Ctrl+C to gracefully shutdown")

	if err := lc.Run(context.Background()); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}
}

// Middleware to count requests
func requestCounterMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&serverStats.Requests, 1)
		atomic.AddInt64(&serverStats.ActiveConns, 1)

		defer func() {
			atomic.AddInt64(&serverStats.ActiveConns, -1)
		}()

		next.ServeHTTP(w, r)
	})
}

// Middleware for request logging
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		next.ServeHTTP(w, r)

		duration := time.Since(start)
		log.Printf("🌐 %s %s - %v", r.Method, r.URL.Path, duration)
	})
}

// Health check handler
func healthHandler(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&serverStats.HealthChecks, 1)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"uptime":    time.Since(serverStats.StartTime).Seconds(),
	})
}

// Status handler showing server statistics
func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"server":  "lifecycle-web-server",
		"version": "1.0.0",
		"stats": map[string]interface{}{
			"start_time":         serverStats.StartTime,
			"uptime_seconds":     time.Since(serverStats.StartTime).Seconds(),
			"total_requests":     atomic.LoadInt64(&serverStats.Requests),
			"active_connections": atomic.LoadInt64(&serverStats.ActiveConns),
			"health_checks":      atomic.LoadInt64(&serverStats.HealthChecks),
		},
		"environment": os.Getenv("ENV"),
	})
}

// Mock users API handler
func usersHandler(w http.ResponseWriter, r *http.Request) {
	// Simulate some processing time
	time.Sleep(100 * time.Millisecond)

	users := []map[string]interface{}{
		{"id": 1, "name": "Alice", "email": "alice@example.com"},
		{"id": 2, "name": "Bob", "email": "bob@example.com"},
		{"id": 3, "name": "Charlie", "email": "charlie@example.com"},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"users":     users,
		"total":     len(users),
		"timestamp": time.Now().Unix(),
	})
}
