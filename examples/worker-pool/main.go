package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/imtanmoy/lifecycle"
)

// Job represents a unit of work
type Job struct {
	ID       int           `json:"id"`
	Type     string        `json:"type"`
	Payload  interface{}   `json:"payload"`
	Duration time.Duration `json:"duration"`
	Created  time.Time     `json:"created"`
}

// JobResult represents the result of processing a job
type JobResult struct {
	JobID     int           `json:"job_id"`
	Success   bool          `json:"success"`
	Result    interface{}   `json:"result,omitempty"`
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	Worker    int           `json:"worker_id"`
	Completed time.Time     `json:"completed"`
}

// WorkerPool manages a pool of workers processing jobs
type WorkerPool struct {
	// Channels for job processing
	jobQueue    chan Job
	resultQueue chan JobResult
	quit        chan struct{}
	
	// Worker management
	workers     []*Worker
	workerCount int
	
	// Statistics
	jobsEnqueued   int64
	jobsProcessed  int64
	jobsSuccessful int64
	jobsFailed     int64
	
	// Job tracking
	mu sync.RWMutex
	activeJobs map[int]Job
	
	// HTTP server for API
	server *http.Server
}

// Worker represents a single worker
type Worker struct {
	ID          int
	jobQueue    chan Job
	resultQueue chan JobResult
	quit        chan struct{}
	isActive    int32
}

func main() {
	workerCount := 5
	queueSize := 100
	
	pool := &WorkerPool{
		jobQueue:    make(chan Job, queueSize),
		resultQueue: make(chan JobResult, queueSize),
		quit:        make(chan struct{}),
		workerCount: workerCount,
		workers:     make([]*Worker, workerCount),
		activeJobs:  make(map[int]Job),
	}

	// Setup HTTP server for job management API
	mux := http.NewServeMux()
	mux.HandleFunc("/submit", pool.submitJobHandler)
	mux.HandleFunc("/status", pool.statusHandler)
	mux.HandleFunc("/stats", pool.statsHandler)
	mux.HandleFunc("/health", pool.healthHandler)
	
	pool.server = &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// Create worker pool lifecycle
	lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
		opts.ShutdownTimeout = 30 * time.Second

		// === PRE-START: Validate configuration ===
		hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
			log.Printf("🔍 Initializing worker pool with %d workers...", workerCount)
			
			if workerCount <= 0 {
				return fmt.Errorf("worker count must be positive, got: %d", workerCount)
			}
			
			if queueSize <= 0 {
				return fmt.Errorf("queue size must be positive, got: %d", queueSize)
			}
			
			log.Printf("✅ Configuration validated - %d workers, queue size %d", workerCount, queueSize)
			return nil
		})

		// === START: Initialize workers and services ===
		
		// Create and start workers
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("👷 Starting worker pool...")
			
			for i := 0; i < workerCount; i++ {
				worker := &Worker{
					ID:          i + 1,
					jobQueue:    pool.jobQueue,
					resultQueue: pool.resultQueue,
					quit:        make(chan struct{}),
				}
				
				pool.workers[i] = worker
				go worker.start()
			}
			
			log.Printf("✅ Started %d workers", workerCount)
			return nil
		})

		// Start result processor
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("📊 Starting result processor...")
			go pool.processResults()
			log.Println("✅ Result processor started")
			return nil
		})

		// Start job generator (for demo purposes)
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("🎲 Starting demo job generator...")
			go pool.generateDemoJobs(ctx)
			log.Println("✅ Demo job generator started")
			return nil
		})

		// Start monitoring
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("📈 Starting monitoring...")
			go pool.monitor(ctx)
			log.Println("✅ Monitoring started")
			return nil
		})

		// === SIGNAL HANDLING ===
		hooks.OnSignal = append(hooks.OnSignal, func(ctx context.Context) error {
			log.Println("📡 Shutdown signal received...")
			log.Printf("📊 Current stats - Enqueued: %d, Processed: %d, Active: %d", 
				atomic.LoadInt64(&pool.jobsEnqueued),
				atomic.LoadInt64(&pool.jobsProcessed),
				len(pool.activeJobs))
			
			return nil
		})

		// === SHUTDOWN: Graceful worker shutdown ===
		hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
			log.Println("🛑 Stopping workers...")
			
			// Close job queue to stop accepting new jobs
			close(pool.jobQueue)
			
			// Wait for workers to finish current jobs
			var wg sync.WaitGroup
			for _, worker := range pool.workers {
				wg.Add(1)
				go func(w *Worker) {
					defer wg.Done()
					close(w.quit)
					
					// Wait for worker to finish with timeout
					ticker := time.NewTicker(100 * time.Millisecond)
					defer ticker.Stop()
					
					timeout := time.After(10 * time.Second)
					for {
						select {
						case <-ticker.C:
							if atomic.LoadInt32(&w.isActive) == 0 {
								return
							}
						case <-timeout:
							log.Printf("⚠️  Worker %d shutdown timeout", w.ID)
							return
						case <-ctx.Done():
							return
						}
					}
				}(worker)
			}
			
			// Wait for all workers to stop
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()
			
			select {
			case <-done:
				log.Println("✅ All workers stopped gracefully")
			case <-ctx.Done():
				log.Println("⚠️  Worker shutdown timeout")
			}
			
			// Signal result processor to stop
			close(pool.quit)
			
			return nil
		})

		// === EXIT: Final cleanup and statistics ===
		hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
			log.Println("📊 Final Statistics:")
			
			enqueued := atomic.LoadInt64(&pool.jobsEnqueued)
			processed := atomic.LoadInt64(&pool.jobsProcessed)
			successful := atomic.LoadInt64(&pool.jobsSuccessful)
			failed := atomic.LoadInt64(&pool.jobsFailed)
			
			log.Printf("   Jobs Enqueued: %d", enqueued)
			log.Printf("   Jobs Processed: %d", processed)
			log.Printf("   Successful: %d", successful)
			log.Printf("   Failed: %d", failed)
			
			if processed > 0 {
				successRate := float64(successful) / float64(processed) * 100
				log.Printf("   Success Rate: %.1f%%", successRate)
			}
			
			pool.mu.RLock()
			activeCount := len(pool.activeJobs)
			pool.mu.RUnlock()
			
			if activeCount > 0 {
				log.Printf("   ⚠️  %d jobs were still active during shutdown", activeCount)
			}
			
			log.Println("✨ Worker pool shutdown complete!")
			return nil
		})
		
	}).AttachHTTPServer(pool.server)

	// Start the worker pool
	log.Println("🌟 Worker Pool Service Starting...")
	log.Printf("🌐 Management API: http://localhost%s", pool.server.Addr)
	log.Println("📋 Available endpoints:")
	log.Println("   POST /submit - Submit a new job")
	log.Println("   GET /status - Pool status")
	log.Println("   GET /stats - Processing statistics")
	log.Println("   GET /health - Health check")
	log.Printf("👷 Worker pool: %d workers, queue capacity: %d", workerCount, queueSize)
	log.Println("💡 Press Ctrl+C to gracefully shutdown")

	if err := lc.Run(context.Background()); err != nil {
		log.Fatalf("❌ Worker pool failed: %v", err)
	}
}

// Worker implementation
func (w *Worker) start() {
	atomic.StoreInt32(&w.isActive, 1)
	defer atomic.StoreInt32(&w.isActive, 0)
	
	log.Printf("👷 Worker %d started", w.ID)
	
	for {
		select {
		case job, ok := <-w.jobQueue:
			if !ok {
				log.Printf("👷 Worker %d: job queue closed", w.ID)
				return
			}
			
			// Process the job
			result := w.processJob(job)
			
			// Send result
			select {
			case w.resultQueue <- result:
			case <-w.quit:
				return
			}
			
		case <-w.quit:
			log.Printf("👷 Worker %d stopped", w.ID)
			return
		}
	}
}

func (w *Worker) processJob(job Job) JobResult {
	start := time.Now()
	
	log.Printf("👷 Worker %d processing job %d (%s)", w.ID, job.ID, job.Type)
	
	// Simulate job processing
	time.Sleep(job.Duration)
	
	// Simulate random success/failure (90% success rate)
	success := rand.Float32() < 0.9
	
	result := JobResult{
		JobID:     job.ID,
		Success:   success,
		Duration:  time.Since(start),
		Worker:    w.ID,
		Completed: time.Now(),
	}
	
	if success {
		result.Result = fmt.Sprintf("Job %d completed successfully by worker %d", job.ID, w.ID)
	} else {
		result.Error = fmt.Sprintf("Job %d failed during processing", job.ID)
	}
	
	return result
}

// Worker pool methods
func (wp *WorkerPool) processResults() {
	for {
		select {
		case result := <-wp.resultQueue:
			wp.handleJobResult(result)
		case <-wp.quit:
			log.Println("📊 Result processor stopped")
			return
		}
	}
}

func (wp *WorkerPool) handleJobResult(result JobResult) {
	// Remove from active jobs
	wp.mu.Lock()
	delete(wp.activeJobs, result.JobID)
	wp.mu.Unlock()
	
	// Update statistics
	atomic.AddInt64(&wp.jobsProcessed, 1)
	
	if result.Success {
		atomic.AddInt64(&wp.jobsSuccessful, 1)
		log.Printf("✅ Job %d completed by worker %d (%v)", result.JobID, result.Worker, result.Duration)
	} else {
		atomic.AddInt64(&wp.jobsFailed, 1)
		log.Printf("❌ Job %d failed on worker %d: %s", result.JobID, result.Worker, result.Error)
	}
}

func (wp *WorkerPool) generateDemoJobs(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	
	jobID := 1
	
	for {
		select {
		case <-ticker.C:
			// Create a demo job
			job := Job{
				ID:       jobID,
				Type:     "demo",
				Payload:  map[string]interface{}{"message": fmt.Sprintf("Demo job #%d", jobID)},
				Duration: time.Duration(rand.Intn(2000)+500) * time.Millisecond, // 0.5-2.5 seconds
				Created:  time.Now(),
			}
			
			wp.submitJob(job)
			jobID++
			
		case <-ctx.Done():
			log.Println("🎲 Demo job generator stopped")
			return
		}
	}
}

func (wp *WorkerPool) submitJob(job Job) {
	// Add to active jobs
	wp.mu.Lock()
	wp.activeJobs[job.ID] = job
	wp.mu.Unlock()
	
	// Send to job queue
	select {
	case wp.jobQueue <- job:
		atomic.AddInt64(&wp.jobsEnqueued, 1)
		log.Printf("📝 Job %d enqueued (%s)", job.ID, job.Type)
	default:
		// Queue is full
		wp.mu.Lock()
		delete(wp.activeJobs, job.ID)
		wp.mu.Unlock()
		log.Printf("⚠️  Job %d rejected - queue full", job.ID)
	}
}

func (wp *WorkerPool) monitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			wp.mu.RLock()
			active := len(wp.activeJobs)
			wp.mu.RUnlock()
			
			processed := atomic.LoadInt64(&wp.jobsProcessed)
			enqueued := atomic.LoadInt64(&wp.jobsEnqueued)
			
			log.Printf("📈 Status: %d active, %d processed, %d enqueued", active, processed, enqueued)
			
		case <-ctx.Done():
			log.Println("📈 Monitor stopped")
			return
		}
	}
}

// HTTP handlers
func (wp *WorkerPool) submitJobHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var req struct {
		Type     string      `json:"type"`
		Payload  interface{} `json:"payload"`
		Duration int         `json:"duration_ms"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	if req.Duration == 0 {
		req.Duration = 1000 // Default 1 second
	}
	
	job := Job{
		ID:       int(atomic.AddInt64(&wp.jobsEnqueued, 0)) + 1000, // Unique ID
		Type:     req.Type,
		Payload:  req.Payload,
		Duration: time.Duration(req.Duration) * time.Millisecond,
		Created:  time.Now(),
	}
	
	wp.submitJob(job)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "submitted",
		"job_id": job.ID,
	})
}

func (wp *WorkerPool) statusHandler(w http.ResponseWriter, r *http.Request) {
	wp.mu.RLock()
	activeJobs := make([]Job, 0, len(wp.activeJobs))
	for _, job := range wp.activeJobs {
		activeJobs = append(activeJobs, job)
	}
	wp.mu.RUnlock()
	
	status := map[string]interface{}{
		"workers":      wp.workerCount,
		"queue_size":   cap(wp.jobQueue),
		"queue_length": len(wp.jobQueue),
		"active_jobs":  len(activeJobs),
		"jobs":         activeJobs,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (wp *WorkerPool) statsHandler(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{
		"enqueued":   atomic.LoadInt64(&wp.jobsEnqueued),
		"processed":  atomic.LoadInt64(&wp.jobsProcessed),
		"successful": atomic.LoadInt64(&wp.jobsSuccessful),
		"failed":     atomic.LoadInt64(&wp.jobsFailed),
		"timestamp":  time.Now().Unix(),
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (wp *WorkerPool) healthHandler(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":   "healthy",
		"workers":  wp.workerCount,
		"uptime":   time.Now().Unix(), // Simplified
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}