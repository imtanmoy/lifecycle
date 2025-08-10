// Package lifecycle provides a structured approach to application lifecycle management
// with signal handling and graceful shutdown capabilities, inspired by Huma's pattern.
//
// The lifecycle follows this execution order:
//  1. OnPreStart - Configuration validation, dependency injection
//  2. OnStart - Server/service startup
//  3. [Wait for signal or context cancellation]
//  4. OnSignal - Signal received notification
//  5. OnShutdown - Graceful shutdown (with timeout)
//  6. OnExit - Final cleanup (always runs, even if shutdown fails)
//
// Example usage:
//
//	lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
//		opts.ShutdownTimeout = 30 * time.Second
//	})
//
//	lc.OnStart(func(ctx context.Context) error {
//		// Start your services
//		return nil
//	}).OnShutdown(func(ctx context.Context) error {
//		// Graceful shutdown
//		return nil
//	})
//
//	if err := lc.Run(context.Background()); err != nil {
//		log.Fatal(err)
//	}
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

// Options defines configuration for signal handling.
type Options struct {
	ShutdownTimeout time.Duration `doc:"Maximum time to wait for graceful shutdown" default:"30s"`
	EnableSIGHUP    bool          `doc:"Listen for SIGHUP signals" default:"true"`
	EnableSIGINT    bool          `doc:"Listen for SIGINT signals" default:"true"`
	EnableSIGTERM   bool          `doc:"Listen for SIGTERM signals" default:"true"`
	EnableSIGQUIT   bool          `doc:"Listen for SIGQUIT signals" default:"true"`
}

// HookFunc represents a lifecycle hook function.
type HookFunc func(ctx context.Context) error

// Hooks contains collections of lifecycle hooks similar to Huma's approach.
type Hooks struct {
	OnPreStart []HookFunc // Called before application startup (e.g., DI wiring, config validation)
	OnStart    []HookFunc // Called during application startup (e.g., start servers/goroutines)
	OnSignal   []HookFunc // Called when a signal is received
	OnShutdown []HookFunc // Called during shutdown process (graceful stop)
	OnExit     []HookFunc // Called after shutdown is complete (final cleanup)
}

// Lifecycle manages signal handling with lifecycle hooks using Huma's pattern.
type Lifecycle struct {
	options Options
	hooks   Hooks
	signals []os.Signal

	running int32 // 0 = not running, 1 = running
}

var ErrAlreadyRunning = fmt.Errorf("lifecycle: Run already in progress")

// New creates a new lifecycle with options - similar to Huma's New function.
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

// Default creates a new lifecycle with default configuration.
func Default() *Lifecycle {
	return New(func(_ *Hooks, _ *Options) {
		// Use defaults
	})
}

// WithSignals allows customizing which signals to listen for.
func (r *Lifecycle) WithSignals(signals ...os.Signal) *Lifecycle {
	r.signals = append([]os.Signal(nil), signals...)
	return r
}

// Listen starts listening for shutdown signals and returns a channel
// that will receive the first signal. The returned function should be
// called to stop signal listening and clean up resources.
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

// Run starts the application lifecycle with hooks
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

// OnPreStart registers hooks to run before application startup.
func (r *Lifecycle) OnPreStart(hooks ...HookFunc) *Lifecycle {
	r.hooks.OnPreStart = append(r.hooks.OnPreStart, hooks...)
	return r
}

// OnStart registers hooks to run during application startup.
func (r *Lifecycle) OnStart(hooks ...HookFunc) *Lifecycle {
	r.hooks.OnStart = append(r.hooks.OnStart, hooks...)
	return r
}

// OnSignal registers hooks to run when a signal is received.
func (r *Lifecycle) OnSignal(hooks ...HookFunc) *Lifecycle {
	r.hooks.OnSignal = append(r.hooks.OnSignal, hooks...)
	return r
}

// OnShutdown registers hooks to run during the shutdown process.
func (r *Lifecycle) OnShutdown(hooks ...HookFunc) *Lifecycle {
	r.hooks.OnShutdown = append(r.hooks.OnShutdown, hooks...)
	return r
}

// OnExit registers hooks to run after shutdown is complete (cleanup hooks).
func (r *Lifecycle) OnExit(hooks ...HookFunc) *Lifecycle {
	r.hooks.OnExit = append(r.hooks.OnExit, hooks...)
	return r
}

// AttachHTTPServer wires an *http.Server* into the lifecycle.
//   - OnStart: starts the server in a goroutine.
//   - OnShutdown: gracefully shuts it down with the runner's shutdown timeout.
//
// Usage:
//
//	srv := &http.Server{Addr: ":8080", Handler: mux}
//	lifecycle.New(func(h *lifecycle.Hooks, o *lifecycle.Options){}).AttachHTTPServer(srv).Run(ctx)
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

// Go runs a long-lived goroutine tied to the lifecycle.
// The goroutine should respect ctx.Done(). On shutdown, the context is canceled and
// we wait (up to the lifecycle's shutdown timeout) for the goroutine to exit.
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
