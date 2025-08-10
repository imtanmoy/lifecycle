// Package lifecycle provides a structured approach to application lifecycle management
// with signal handling and graceful shutdown capabilities.
//
// # Overview
//
// This package helps you build robust applications that can start up cleanly, run
// reliably, and shut down gracefully. It provides structured lifecycle hooks that
// execute in a predictable order, making it easy to manage resources like HTTP
// servers, database connections, and background workers.
//
// # Lifecycle Flow
//
// The lifecycle follows this execution order:
//  1. OnPreStart - Configuration validation, dependency injection
//  2. OnStart - Server/service startup
//  3. [Wait for signal or context cancellation]
//  4. OnSignal - Signal received notification
//  5. OnShutdown - Graceful shutdown (with timeout)
//  6. OnExit - Final cleanup (always runs, even if shutdown fails)
//
// # Basic Usage
//
// Create a lifecycle with custom configuration:
//
//	lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
//		opts.ShutdownTimeout = 30 * time.Second
//		
//		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
//			log.Println("Application started")
//			return nil
//		})
//		
//		hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
//			log.Println("Shutting down gracefully...")
//			return nil
//		})
//	})
//
//	if err := lc.Run(context.Background()); err != nil {
//		log.Fatal(err)
//	}
//
// # HTTP Server Integration
//
// Easily attach HTTP servers to the lifecycle:
//
//	server := &http.Server{Addr: ":8080", Handler: mux}
//	lc := lifecycle.Default().AttachHTTPServer(server)
//	if err := lc.Run(context.Background()); err != nil {
//		log.Fatal(err)
//	}
//
// # Background Workers
//
// Run background goroutines that respect context cancellation:
//
//	lc := lifecycle.Default().Go(func(ctx context.Context) error {
//		ticker := time.NewTicker(5 * time.Second)
//		defer ticker.Stop()
//		
//		for {
//			select {
//			case <-ticker.C:
//				log.Println("Background task running...")
//			case <-ctx.Done():
//				return ctx.Err()
//			}
//		}
//	})
//
// # Signal Handling
//
// By default, the lifecycle listens for SIGINT, SIGTERM, SIGHUP, and SIGQUIT
// signals. You can customize which signals to handle:
//
//	lc := lifecycle.Default().WithSignals(syscall.SIGINT, syscall.SIGTERM)
//
// # Error Handling
//
// The lifecycle preserves errors from both shutdown and exit phases using
// Go 1.20's errors.Join(), allowing you to handle multiple failure scenarios:
//
//	if err := lc.Run(ctx); err != nil {
//		if errors.Is(err, someShutdownError) {
//			log.Println("Shutdown failed")
//		}
//		log.Printf("Lifecycle error: %v", err)
//	}
//
// # Key Guarantees
//
//   - OnExit hooks ALWAYS execute, even if OnShutdown fails
//   - Shutdown timeout enforcement prevents hanging
//   - Thread-safe execution with concurrent protection
//   - Proper resource cleanup on early cancellation
//   - Error preservation from all phases
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"
)

// Options configures lifecycle behavior including shutdown timeouts and signal handling.
// All options have sensible defaults and are optional to configure.
//
// Example:
//
//	lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
//		opts.ShutdownTimeout = 45 * time.Second // Custom timeout
//		opts.EnableSIGHUP = false              // Disable SIGHUP handling
//	})
type Options struct {
	// ShutdownTimeout is the maximum time to wait for graceful shutdown.
	// If shutdown takes longer than this duration, the lifecycle will
	// return with a context.DeadlineExceeded error. Default: 30 seconds.
	ShutdownTimeout time.Duration `doc:"Maximum time to wait for graceful shutdown" default:"30s"`
	
	// EnableSIGHUP controls whether to listen for SIGHUP signals.
	// SIGHUP is commonly used for configuration reloads. Default: true.
	EnableSIGHUP    bool          `doc:"Listen for SIGHUP signals" default:"true"`
	
	// EnableSIGINT controls whether to listen for SIGINT signals (Ctrl+C).
	// This is the most common way users terminate applications. Default: true.
	EnableSIGINT    bool          `doc:"Listen for SIGINT signals" default:"true"`
	
	// EnableSIGTERM controls whether to listen for SIGTERM signals.
	// SIGTERM is the standard termination signal used by process managers
	// and container orchestrators. Default: true.
	EnableSIGTERM   bool          `doc:"Listen for SIGTERM signals" default:"true"`
	
	// EnableSIGQUIT controls whether to listen for SIGQUIT signals (Ctrl+\).
	// SIGQUIT typically requests a clean shutdown with core dump. Default: true.
	EnableSIGQUIT   bool          `doc:"Listen for SIGQUIT signals" default:"true"`
}

// HookFunc represents a lifecycle hook function that receives a context
// and returns an error. Hook functions should respect context cancellation
// and return promptly when the context is canceled.
//
// Example hook function:
//
//	func(ctx context.Context) error {
//		log.Println("Performing startup task...")
//		select {
//		case <-time.After(2 * time.Second):
//			return nil // Task completed
//		case <-ctx.Done():
//			return ctx.Err() // Context canceled
//		}
//	}
type HookFunc func(ctx context.Context) error

// Hooks contains collections of lifecycle hook functions that execute at
// different phases of the application lifecycle. Each slice of hooks is
// executed in order during its respective phase.
//
// Hook execution phases:
//   - OnPreStart: Before any services start (config validation, DI setup)
//   - OnStart: During startup (start servers, spawn goroutines)
//   - OnSignal: When shutdown signal received (logging, notifications)
//   - OnShutdown: During graceful shutdown (close connections, save state)
//   - OnExit: Final cleanup phase (always runs, resource deallocation)
//
// Example usage:
//
//	hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
//		log.Println("Starting HTTP server...")
//		return startServer(ctx)
//	})
//	
//	hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
//		log.Println("Cleaning up resources...")
//		return cleanup()
//	})
type Hooks struct {
	// OnPreStart hooks execute before application startup.
	// Use for configuration validation, dependency injection setup,
	// database connection establishment, etc.
	OnPreStart []HookFunc
	
	// OnStart hooks execute during application startup.
	// Use for starting HTTP servers, spawning background goroutines,
	// initializing services, etc.
	OnStart    []HookFunc
	
	// OnSignal hooks execute immediately when a shutdown signal is received.
	// Use for logging shutdown reason, sending notifications, quick cleanup tasks.
	// Keep these hooks fast as they delay the actual shutdown process.
	OnSignal   []HookFunc
	
	// OnShutdown hooks execute during the graceful shutdown process.
	// They have a timeout (configured via Options.ShutdownTimeout).
	// Use for gracefully stopping servers, closing connections, saving state.
	OnShutdown []HookFunc
	
	// OnExit hooks execute after shutdown is complete and ALWAYS run,
	// even if OnShutdown hooks fail or timeout. Use for final resource
	// cleanup, closing file handles, releasing memory, etc.
	OnExit     []HookFunc
}

// Lifecycle manages the application lifecycle with structured hooks and signal handling.
// It provides a clean way to start services, wait for shutdown signals, and perform
// graceful cleanup. The Lifecycle is reusable - you can call Run() multiple times
// after previous runs complete.
//
// Lifecycle guarantees:
//   - OnExit hooks always execute, even if shutdown fails
//   - Shutdown operations respect the configured timeout
//   - Thread-safe execution prevents concurrent Run() calls
//   - Context cancellation is properly handled throughout
//   - Errors from all phases are preserved and returned
type Lifecycle struct {
	options Options
	hooks   Hooks
	signals []os.Signal

	running int32 // 0 = not running, 1 = running
}

// ErrAlreadyRunning is returned when Run() is called while another Run() is already in progress.
// The Lifecycle prevents concurrent execution to avoid resource conflicts and unpredictable behavior.
var ErrAlreadyRunning = fmt.Errorf("lifecycle: Run already in progress")

// New creates a new Lifecycle with custom configuration. The configure function
// receives pointers to Hooks and Options structures that you can modify to
// customize the lifecycle behavior.
//
// The configure function is called immediately during New(), allowing you to:
//   - Set custom shutdown timeout
//   - Enable/disable specific signals
//   - Register initial hooks
//
// Example:
//
//	lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
//		// Custom timeout
//		opts.ShutdownTimeout = 45 * time.Second
//		
//		// Disable specific signals
//		opts.EnableSIGHUP = false
//		
//		// Register hooks
//		hooks.OnStart = append(hooks.OnStart, startDatabase)
//		hooks.OnExit = append(hooks.OnExit, cleanupResources)
//	})
func New(configure func(hooks *Hooks, opts *Options)) *Lifecycle {
	opts := &Options{
		ShutdownTimeout: 30 * time.Second,
		EnableSIGHUP:    true,
		EnableSIGINT:    true,
		EnableSIGTERM:   true,
		EnableSIGQUIT:   true,
	}

	hooks := &Hooks{}

	// Call the configuration function
	configure(hooks, opts)

	// Build signals list based on options
	signals := defaultSignals(opts)

	// Fallback to safe defaults if no signals configured
	// This prevents signal.Notify() from subscribing to all signals
	if len(signals) == 0 {
		signals = []os.Signal{os.Interrupt}
	}

	return &Lifecycle{
		options: *opts,
		hooks:   *hooks,
		signals: signals,
	}
}

// Default creates a new Lifecycle with all default settings.
// This is equivalent to calling New() with an empty configuration function.
//
// Default settings:
//   - ShutdownTimeout: 30 seconds
//   - All signals enabled (SIGHUP, SIGINT, SIGTERM, SIGQUIT)
//   - No hooks registered
//
// Example:
//
//	lc := lifecycle.Default()
//	lc.OnStart(startServices).OnShutdown(stopServices)
//	if err := lc.Run(ctx); err != nil {
//		log.Fatal(err)
//	}
func Default() *Lifecycle {
	return New(func(_ *Hooks, _ *Options) {
		// Use defaults
	})
}

// WithSignals customizes which OS signals the lifecycle should listen for.
// This overrides the default signal configuration and any Options.Enable* settings.
//
// Use this when you need fine-grained control over signal handling, such as:
//   - Adding custom signals like SIGUSR1, SIGUSR2
//   - Supporting only a subset of signals
//   - Cross-platform compatibility (Windows only supports os.Interrupt)
//
// Example:
//
//	// Unix-specific signals
//	lc := lifecycle.Default().WithSignals(
//		syscall.SIGINT,
//		syscall.SIGTERM,
//		syscall.SIGUSR1, // Custom application signal
//	)
//	
//	// Windows-compatible
//	lc := lifecycle.Default().WithSignals(os.Interrupt)
func (r *Lifecycle) WithSignals(signals ...os.Signal) *Lifecycle {
	r.signals = append([]os.Signal(nil), signals...)
	return r
}

// Listen starts listening for OS signals and returns a channel that will
// receive the first signal, plus a cleanup function to stop listening.
//
// This method is primarily used internally by Run(), but can be useful
// for custom lifecycle management or testing scenarios.
//
// The returned channel is buffered with capacity 1 to avoid missing signals.
// The cleanup function should always be called to properly stop signal
// notification and close the channel.
//
// Example:
//
//	sigChan, cleanup := lc.Listen()
//	defer cleanup()
//	
//	select {
//	case sig := <-sigChan:
//		log.Printf("Received signal: %v", sig)
//	case <-ctx.Done():
//		log.Println("Context canceled")
//	}
func (r *Lifecycle) Listen() (<-chan os.Signal, func()) {
	sigChan := make(chan os.Signal, 1)
	if len(r.signals) > 0 {
		signal.Notify(sigChan, r.signals...)
	}

	cleanup := func() {
		if len(r.signals) > 0 {
			signal.Stop(sigChan)
		}
		close(sigChan)
	}
	return sigChan, cleanup
}

// Run executes the complete application lifecycle with all registered hooks.
// This is the main entry point for starting your application.
//
// Execution flow:
//  1. Execute OnPreStart hooks
//  2. Execute OnStart hooks  
//  3. Wait for OS signal or context cancellation
//  4. Execute OnSignal hooks
//  5. Execute OnShutdown hooks (with timeout)
//  6. Execute OnExit hooks (always runs)
//
// Run() is thread-safe and prevents concurrent execution. If called while
// another Run() is in progress, it returns ErrAlreadyRunning immediately.
//
// The context parameter controls the overall lifecycle timeout and can be
// used to programmatically trigger shutdown. If the context is canceled,
// the lifecycle will proceed to the shutdown phase.
//
// Returns an error if any hooks fail. Errors from both OnShutdown and OnExit
// phases are preserved using errors.Join().
//
// Example:
//
//	ctx := context.Background()
//	if err := lc.Run(ctx); err != nil {
//		if errors.Is(err, context.DeadlineExceeded) {
//			log.Println("Shutdown timed out")
//		}
//		log.Fatalf("Lifecycle failed: %v", err)
//	}
func (r *Lifecycle) Run(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&r.running, 0, 1) {
		return ErrAlreadyRunning
	}
	// allow reuse after completion
	defer atomic.StoreInt32(&r.running, 0)

	return r.run(ctx)
}

func (r *Lifecycle) run(ctx context.Context) error {
	// Run OnPreStart hooks
	if err := r.runHooks(ctx, r.hooks.OnPreStart); err != nil {
		return err
	}

	// Run OnStart hooks
	if err := r.runHooks(ctx, r.hooks.OnStart); err != nil {
		return err
	}

	// Wait for signal or context cancellation
	sigChan, cleanup := r.Listen()
	defer cleanup()

	select {
	case <-sigChan:
		// OS signal received
	case <-ctx.Done():
		// Context cancelled
	}

	// Run OnSignal hooks
	if err := r.runHooks(ctx, r.hooks.OnSignal); err != nil {
		return err
	}

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, r.options.ShutdownTimeout)
	defer cancel()

	// Run OnShutdown hooks
	shutdownErr := r.runHooks(shutdownCtx, r.hooks.OnShutdown)

	// Always run OnExit hooks, even if OnShutdown failed
	exitErr := r.runHooks(shutdownCtx, r.hooks.OnExit)

	// return both if present (nil values are ignored)
	return errors.Join(shutdownErr, exitErr)
}

// runHooks executes a slice of hooks and returns the first error encountered.
func (r *Lifecycle) runHooks(ctx context.Context, hooks []HookFunc) error {
	if len(hooks) == 0 {
		return nil
	}

	for i, hook := range hooks {
		if err := hook(ctx); err != nil {
			// Add context to help with debugging which hook failed
			return fmt.Errorf("hook %d failed: %w", i+1, err)
		}
	}
	return nil
}

// OnPreStart registers hook functions to execute before application startup.
// These hooks are ideal for configuration validation, dependency injection,
// database connections, and other initialization tasks.
//
// OnPreStart hooks execute synchronously in the order they were registered.
// If any hook returns an error, the lifecycle stops immediately without
// proceeding to OnStart hooks.
//
// Example:
//
//	lc.OnPreStart(
//		validateConfig,
//		connectDatabase,
//		initializeServices,
//	)
func (r *Lifecycle) OnPreStart(hooks ...HookFunc) *Lifecycle {
	r.hooks.OnPreStart = append(r.hooks.OnPreStart, hooks...)
	return r
}

// OnStart registers hook functions to execute during application startup.
// These hooks are ideal for starting HTTP servers, spawning background
// goroutines, and initializing services.
//
// OnStart hooks execute synchronously in the order they were registered.
// If any hook returns an error, the lifecycle stops and proceeds to shutdown.
//
// For long-running operations, consider using the Go() method instead, which
// manages goroutine lifecycles automatically.
//
// Example:
//
//	lc.OnStart(
//		startHTTPServer,
//		startMetricsServer,
//		initializeWorkers,
//	)
func (r *Lifecycle) OnStart(hooks ...HookFunc) *Lifecycle {
	r.hooks.OnStart = append(r.hooks.OnStart, hooks...)
	return r
}

// OnSignal registers hook functions to execute immediately when an OS signal
// is received. These hooks run before the shutdown process begins.
//
// Keep OnSignal hooks fast and lightweight since they delay the start of
// graceful shutdown. Use them for logging, notifications, or quick state changes.
//
// OnSignal hooks execute synchronously in the order they were registered.
// Errors from these hooks are logged but don't prevent shutdown from proceeding.
//
// Example:
//
//	lc.OnSignal(
//		logShutdownReason,
//		sendShutdownNotification,
//		updateHealthcheck,
//	)
func (r *Lifecycle) OnSignal(hooks ...HookFunc) *Lifecycle {
	r.hooks.OnSignal = append(r.hooks.OnSignal, hooks...)
	return r
}

// OnShutdown registers hook functions to execute during graceful shutdown.
// These hooks have a timeout (configured via Options.ShutdownTimeout) and
// should perform cleanup tasks like stopping servers and closing connections.
//
// OnShutdown hooks execute synchronously in the order they were registered.
// If the timeout expires before all hooks complete, the lifecycle proceeds
// to OnExit hooks with a context.DeadlineExceeded error.
//
// Example:
//
//	lc.OnShutdown(
//		stopHTTPServer,
//		drainConnections,
//		flushBuffers,
//	)
func (r *Lifecycle) OnShutdown(hooks ...HookFunc) *Lifecycle {
	r.hooks.OnShutdown = append(r.hooks.OnShutdown, hooks...)
	return r
}

// OnExit registers hook functions for final cleanup that ALWAYS execute,
// even if OnShutdown hooks fail or timeout. Use these for critical resource
// cleanup like closing files, releasing memory, and disconnecting from databases.
//
// OnExit hooks execute synchronously in the order they were registered.
// They share the same timeout context as OnShutdown hooks.
//
// Since these hooks always run, they're your last chance to clean up resources
// and prevent leaks, so make them as robust as possible.
//
// Example:
//
//	lc.OnExit(
//		closeDatabase,
//		cleanupTempFiles,
//		releaseResources,
//	)
func (r *Lifecycle) OnExit(hooks ...HookFunc) *Lifecycle {
	r.hooks.OnExit = append(r.hooks.OnExit, hooks...)
	return r
}

// AttachHTTPServer integrates an http.Server into the lifecycle management.
// This is a convenience method that automatically handles server startup and
// graceful shutdown without requiring you to write boilerplate code.
//
// What it does:
//   - OnStart: Creates a TCP listener and starts the server in a goroutine
//   - OnShutdown: Calls server.Shutdown() with the configured timeout
//
// The server will inherit the lifecycle's context for request handling if no
// BaseContext is already configured.
//
// Multiple HTTP servers can be attached to the same lifecycle - each will be
// managed independently.
//
// Example:
//
//	server := &http.Server{
//		Addr:    ":8080",
//		Handler: mux,
//	}
//	
//	lc := lifecycle.Default().AttachHTTPServer(server)
//	if err := lc.Run(ctx); err != nil {
//		log.Fatal(err)
//	}
//
// For more complex server management, use OnStart and OnShutdown hooks directly.
func (r *Lifecycle) AttachHTTPServer(srv *http.Server) *Lifecycle {
	// Start the HTTP server when the app starts
	r.OnStart(func(ctx context.Context) error {
		ln, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			// Port in use, permissions, invalid address, etc.
			return fmt.Errorf("HTTP listen failed: %w", err)
		}

		// Propagate lifecycle context to request handlers
		if srv.BaseContext == nil {
			srv.BaseContext = func(net.Listener) context.Context { return ctx }
		}

		// If ctx is already cancelled, don't start goroutine; close listener.
		select {
		case <-ctx.Done():
			_ = ln.Close()
			return ctx.Err()
		default:
		}

		go func() {
			// Serve returns http.ErrServerClosed on graceful shutdown
			_ = srv.Serve(ln)
		}()
		return nil
	})

	// Gracefully stop the HTTP server on shutdown
	r.OnShutdown(func(ctx context.Context) error {
		// Use the provided context which already has the configured timeout
		return srv.Shutdown(ctx)
	})

	return r
}

// Go runs a long-lived goroutine that's tied to the application lifecycle.
// This is perfect for background workers, periodic tasks, and other services
// that should run for the application's lifetime.
//
// What it does:
//   - OnStart: Spawns your function in a goroutine with a cancelable context
//   - OnShutdown: Cancels the context and waits for the goroutine to exit
//
// Your function MUST respect ctx.Done() and return promptly when the context
// is canceled, otherwise shutdown will timeout.
//
// Multiple goroutines can be managed by calling Go() multiple times.
//
// Example:
//
//	lc.Go(func(ctx context.Context) error {
//		ticker := time.NewTicker(30 * time.Second)
//		defer ticker.Stop()
//		
//		for {
//			select {
//			case <-ticker.C:
//				// Do periodic work
//				if err := performTask(); err != nil {
//					log.Printf("Task failed: %v", err)
//				}
//			case <-ctx.Done():
//				log.Println("Worker shutting down")
//				return ctx.Err()
//			}
//		}
//	})
func (r *Lifecycle) Go(run func(ctx context.Context) error) *Lifecycle {
	var wg sync.WaitGroup
	var cancel context.CancelFunc

	r.OnStart(func(ctx context.Context) error {
		childCtx, c := context.WithCancel(ctx)
		cancel = c
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = run(childCtx)
		}()
		return nil
	})

	r.OnShutdown(func(ctx context.Context) error {
		if cancel != nil {
			cancel()
		}
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	return r
}
