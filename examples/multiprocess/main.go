package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/imtanmoy/lifecycle"
)

func main() {
	log.Println("Starting multiprocess example with two HTTP servers and one worker")

	// Create first HTTP server (API server)
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status": "healthy", "server": "api"}`))
	})
	apiMux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id": 1, "name": "Alice"}, {"id": 2, "name": "Bob"}]`))
	})

	apiServer := &http.Server{
		Addr:    ":8080",
		Handler: apiMux,
	}

	// Create second HTTP server (Admin server)
	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/admin/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status": "healthy", "server": "admin"}`))
	})
	adminMux.HandleFunc("/admin/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"requests": 42, "uptime": "5m30s"}`))
	})

	adminServer := &http.Server{
		Addr:    ":8081",
		Handler: adminMux,
	}

	// Create lifecycle with multiple services
	lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
		opts.ShutdownTimeout = 30 * time.Second

		hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
			log.Println("Validating multiprocess configuration...")
			return nil
		})

		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("All services started successfully")
			log.Println("API Server available at: http://localhost:8080")
			log.Println("  - GET /api/health")
			log.Println("  - GET /api/users")
			log.Println("Admin Server available at: http://localhost:8081")
			log.Println("  - GET /admin/health")
			log.Println("  - GET /admin/metrics")
			return nil
		})

		hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
			log.Println("Shutting down all services gracefully...")
			return nil
		})

		hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
			log.Println("All services stopped and cleanup completed")
			return nil
		})
	}).
		// Attach both HTTP servers
		AttachHTTPServer(apiServer).
		AttachHTTPServer(adminServer).
		// Add background worker goroutine
		Go(func(ctx context.Context) error {
			log.Println("Background worker started")
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					log.Println("Worker: Performing background task (cleanup, metrics collection, etc.)")
				case <-ctx.Done():
					log.Println("Worker: Stopping background task...")
					return ctx.Err()
				}
			}
		})

	// Start all services
	if err := lc.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}