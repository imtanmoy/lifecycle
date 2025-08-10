package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/imtanmoy/lifecycle"
	_ "github.com/mattn/go-sqlite3" // SQLite driver for demo
)

// UserService represents our microservice
type UserService struct {
	db       *sql.DB
	cache    *Cache
	mu       sync.RWMutex
	users    map[int]User
	shutdown chan struct{}
}

// User represents a user entity
type User struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Created string `json:"created"`
}

// Simple in-memory cache
type Cache struct {
	mu   sync.RWMutex
	data map[string]interface{}
}

func NewCache() *Cache {
	return &Cache{
		data: make(map[string]interface{}),
	}
}

func (c *Cache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = value
}

func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, exists := c.data[key]
	return value, exists
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]interface{})
}

func main() {
	service := &UserService{
		users:    make(map[int]User),
		shutdown: make(chan struct{}),
	}

	// HTTP server setup
	mux := http.NewServeMux()
	mux.HandleFunc("/health", service.healthHandler)
	mux.HandleFunc("/users", service.usersHandler)
	mux.HandleFunc("/users/", service.userHandler)
	mux.HandleFunc("/cache/stats", service.cacheStatsHandler)

	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// Create comprehensive microservice lifecycle
	lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
		opts.ShutdownTimeout = 45 * time.Second

		// === PRE-START: Validate environment and setup ===
		hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
			log.Println("🔍 Pre-start: Validating environment...")

			// Check required environment variables would go here
			log.Println("✅ Environment validation complete")
			return nil
		})

		// === START PHASE: Initialize all services ===

		// Initialize database
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("🗄️  Initializing database connection...")

			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				return fmt.Errorf("failed to open database: %w", err)
			}
			service.db = db

			// Create tables
			if err := service.createTables(ctx); err != nil {
				return fmt.Errorf("failed to create tables: %w", err)
			}

			// Seed initial data
			if err := service.seedData(ctx); err != nil {
				return fmt.Errorf("failed to seed data: %w", err)
			}

			log.Println("✅ Database initialized successfully")
			return nil
		})

		// Initialize cache
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("🧠 Initializing cache...")
			service.cache = NewCache()

			// Pre-warm cache with some data
			service.cache.Set("startup_time", time.Now())
			service.cache.Set("service_name", "user-microservice")

			log.Println("✅ Cache initialized successfully")
			return nil
		})

		// Start background workers
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("🔄 Starting background workers...")
			return nil
		})

		// === ATTACH HTTP SERVER ===
		// This automatically handles HTTP server lifecycle

		// === BACKGROUND TASKS ===

		// Cache cleanup worker
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			go service.cacheCleanupWorker(ctx)
			log.Println("✅ Cache cleanup worker started")
			return nil
		})

		// Metrics collection worker
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			go service.metricsWorker(ctx)
			log.Println("✅ Metrics collection worker started")
			return nil
		})

		// Health check worker
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			go service.healthCheckWorker(ctx)
			log.Println("✅ Health check worker started")
			return nil
		})

		// === SIGNAL HANDLING ===
		hooks.OnSignal = append(hooks.OnSignal, func(ctx context.Context) error {
			log.Println("📡 Shutdown signal received...")
			log.Println("🔄 Stopping new request processing...")

			// Signal all background workers to stop
			close(service.shutdown)

			return nil
		})

		// === SHUTDOWN PHASE: Graceful cleanup ===

		// Stop background services
		hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
			log.Println("🛑 Stopping background services...")

			// Wait for background workers to finish (with timeout)
			select {
			case <-time.After(10 * time.Second):
				log.Println("⚠️  Background workers shutdown timeout")
			case <-ctx.Done():
				log.Println("🔄 Background workers stopped")
			}

			return nil
		})

		// Close database connections
		hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
			log.Println("🗄️  Closing database connections...")

			if service.db != nil {
				if err := service.db.Close(); err != nil {
					return fmt.Errorf("failed to close database: %w", err)
				}
				log.Println("✅ Database connections closed")
			}

			return nil
		})

		// === EXIT PHASE: Final cleanup ===
		hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
			log.Println("🧹 Final cleanup...")

			// Clear cache
			if service.cache != nil {
				service.cache.Clear()
				log.Println("✅ Cache cleared")
			}

			// Final statistics
			if startTime, exists := service.cache.Get("startup_time"); exists {
				if st, ok := startTime.(time.Time); ok {
					uptime := time.Since(st)
					log.Printf("📊 Service ran for: %v", uptime)
				}
			}

			log.Println("✨ User microservice shutdown complete!")
			return nil
		})

	}).AttachHTTPServer(httpServer)

	// Start the microservice
	log.Println("🌟 User Microservice Starting...")
	log.Printf("🌐 HTTP API: http://localhost%s", httpServer.Addr)
	log.Println("📋 Available endpoints:")
	log.Println("   GET /health - Service health")
	log.Println("   GET /users - List all users")
	log.Println("   GET /users/{id} - Get specific user")
	log.Println("   GET /cache/stats - Cache statistics")
	log.Println("💡 Press Ctrl+C to gracefully shutdown")

	if err := lc.Run(context.Background()); err != nil {
		log.Fatalf("❌ Microservice failed: %v", err)
	}
}

// Database operations
func (s *UserService) createTables(ctx context.Context) error {
	query := `
	CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		email TEXT UNIQUE NOT NULL,
		created DATETIME DEFAULT CURRENT_TIMESTAMP
	)`

	_, err := s.db.ExecContext(ctx, query)
	return err
}

func (s *UserService) seedData(ctx context.Context) error {
	users := []User{
		{Name: "Alice Johnson", Email: "alice@example.com"},
		{Name: "Bob Smith", Email: "bob@example.com"},
		{Name: "Charlie Brown", Email: "charlie@example.com"},
	}

	for _, user := range users {
		query := "INSERT INTO users (name, email) VALUES (?, ?)"
		_, err := s.db.ExecContext(ctx, query, user.Name, user.Email)
		if err != nil {
			return err
		}
	}

	return nil
}

// HTTP handlers
func (s *UserService) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	health := map[string]interface{}{
		"status":    "healthy",
		"service":   "user-microservice",
		"timestamp": time.Now().Unix(),
		"database":  s.checkDatabase(),
		"cache":     s.checkCache(),
	}

	json.NewEncoder(w).Encode(health)
}

func (s *UserService) usersHandler(w http.ResponseWriter, r *http.Request) {
	// Check cache first
	if cached, exists := s.cache.Get("users_list"); exists {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		json.NewEncoder(w).Encode(cached)
		return
	}

	// Query database
	rows, err := s.db.Query("SELECT id, name, email, created FROM users")
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Created)
		if err != nil {
			continue
		}
		users = append(users, u)
	}

	result := map[string]interface{}{
		"users": users,
		"total": len(users),
	}

	// Cache the result
	s.cache.Set("users_list", result)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")
	json.NewEncoder(w).Encode(result)
}

func (s *UserService) userHandler(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from path (simplified)
	userID := "1" // In real app, parse from URL path

	cacheKey := fmt.Sprintf("user_%s", userID)
	if cached, exists := s.cache.Get(cacheKey); exists {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		json.NewEncoder(w).Encode(cached)
		return
	}

	// Mock user response
	user := User{
		ID:      1,
		Name:    "Alice Johnson",
		Email:   "alice@example.com",
		Created: time.Now().Format(time.RFC3339),
	}

	s.cache.Set(cacheKey, user)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "MISS")
	json.NewEncoder(w).Encode(user)
}

func (s *UserService) cacheStatsHandler(w http.ResponseWriter, r *http.Request) {
	s.cache.mu.RLock()
	size := len(s.cache.data)
	s.cache.mu.RUnlock()

	stats := map[string]interface{}{
		"cache_size": size,
		"timestamp":  time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// Background workers
func (s *UserService) cacheCleanupWorker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Simulate cache cleanup
			log.Println("🧹 Cache cleanup cycle completed")
		case <-s.shutdown:
			log.Println("🛑 Cache cleanup worker stopped")
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *UserService) metricsWorker(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Println("📊 Metrics collection completed")
		case <-s.shutdown:
			log.Println("🛑 Metrics worker stopped")
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *UserService) healthCheckWorker(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Perform internal health checks
			dbOk := s.checkDatabase()
			cacheOk := s.checkCache()
			if !dbOk || !cacheOk {
				log.Println("⚠️  Health check detected issues")
			}
		case <-s.shutdown:
			log.Println("🛑 Health check worker stopped")
			return
		case <-ctx.Done():
			return
		}
	}
}

// Health check utilities
func (s *UserService) checkDatabase() bool {
	if s.db == nil {
		return false
	}
	return s.db.Ping() == nil
}

func (s *UserService) checkCache() bool {
	return s.cache != nil
}
