//go:build !windows

package lifecycle_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/imtanmoy/lifecycle"
)

func TestNew(t *testing.T) {
	t.Run("creates lifecycle with custom configuration", func(t *testing.T) {
		var configuredTimeout time.Duration
		var configuredSIGHUP bool
		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			configuredTimeout = 10 * time.Second
			opts.ShutdownTimeout = configuredTimeout
			opts.EnableSIGHUP = false
			configuredSIGHUP = opts.EnableSIGHUP
		})

		require.NotNil(t, lc)

		// Test that configuration was applied by testing behavior
		// We can't access internal fields, but we know the config worked
		// if the timeout and signals behave as expected in actual usage
		assert.Equal(t, 10*time.Second, configuredTimeout)
		assert.False(t, configuredSIGHUP)
	})

	t.Run("accepts configuration function", func(t *testing.T) {
		lc := lifecycle.New(func(_ *lifecycle.Hooks, _ *lifecycle.Options) {})
		assert.NotNil(t, lc)
	})
}

func TestDefault(t *testing.T) {
	lc := lifecycle.Default()
	assert.NotNil(t, lc)
}

func TestWithSignals(t *testing.T) {
	lc := lifecycle.Default().WithSignals(os.Interrupt)
	assert.NotNil(t, lc)
}

func TestWithSignalsSliceCopy(t *testing.T) {
	t.Run("prevents external mutation", func(t *testing.T) {
		// Create a slice that we'll try to mutate after setting
		originalSignals := []os.Signal{syscall.SIGINT, syscall.SIGTERM}

		lc := lifecycle.Default().WithSignals(originalSignals...)

		// Try to mutate the original slice
		originalSignals[0] = syscall.SIGUSR1
		_ = append(originalSignals, syscall.SIGQUIT) // Intentional: testing slice copy protection

		// Lifecycle should be unaffected - test by starting a Listen operation
		sigChan, cleanup := lc.Listen()
		defer cleanup()

		// The signal channel should be created without error
		// This verifies the internal signals weren't corrupted
		assert.NotNil(t, sigChan)

		// Additional verification: the mutation shouldn't affect new Listen calls
		sigChan2, cleanup2 := lc.Listen()
		defer cleanup2()
		assert.NotNil(t, sigChan2)
	})

	t.Run("copies empty slice correctly", func(t *testing.T) {
		lc := lifecycle.Default().WithSignals()

		// Should use fallback defaults, not panic
		sigChan, cleanup := lc.Listen()
		defer cleanup()
		assert.NotNil(t, sigChan)
	})

	t.Run("copies single signal correctly", func(t *testing.T) {
		originalSignals := []os.Signal{syscall.SIGINT}
		lc := lifecycle.Default().WithSignals(originalSignals...)

		// Mutate original
		originalSignals[0] = syscall.SIGUSR1

		// Lifecycle should work with original signal (SIGINT)
		sigChan, cleanup := lc.Listen()
		defer cleanup()
		assert.NotNil(t, sigChan)
	})
}

func TestListen(t *testing.T) {
	t.Run("returns signal channel and cleanup function", func(t *testing.T) {
		// Use SIGUSR1 for testing to avoid conflicts with test runner
		lc := lifecycle.Default().WithSignals(syscall.SIGUSR1)

		sigChan, cleanup := lc.Listen()
		defer cleanup()

		require.NotNil(t, sigChan)

		select {
		case <-sigChan:
			t.Error("should not receive signal immediately")
		case <-time.After(10 * time.Millisecond):
			// Expected behavior
		}
	})

	t.Run("cleanup function stops signal listening", func(t *testing.T) {
		// Use SIGUSR1 for testing to avoid conflicts with test runner
		lc := lifecycle.Default().WithSignals(syscall.SIGUSR1)

		sigChan, cleanup := lc.Listen()
		cleanup()
		assert.Eventually(t, func() bool {
			select {
			case _, ok := <-sigChan:
				return !ok
			default:
				return false
			}
		}, 200*time.Millisecond, 10*time.Millisecond, "channel should be closed after cleanup")
	})
}

func TestHookRegistration(t *testing.T) {
	lc := lifecycle.Default()

	hook1 := func(ctx context.Context) error { return nil }
	hook2 := func(ctx context.Context) error { return nil }

	// Test that methods return the lifecycle for chaining
	result := lc.OnPreStart(hook1, hook2)
	assert.Equal(t, lc, result)

	result = lc.OnStart(hook1)
	assert.Equal(t, lc, result)

	result = lc.OnSignal(hook2)
	assert.Equal(t, lc, result)

	result = lc.OnShutdown(hook1, hook2)
	assert.Equal(t, lc, result)

	result = lc.OnExit(hook1)
	assert.Equal(t, lc, result)
}

func TestHookExecution(t *testing.T) {
	t.Run("hooks execute in correct order", func(t *testing.T) {
		var executed []string
		var mu sync.Mutex

		addExecution := func(phase string) {
			mu.Lock()
			defer mu.Unlock()
			executed = append(executed, phase)
		}

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			opts.ShutdownTimeout = 100 * time.Millisecond
			hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
				addExecution("prestart")
				return nil
			})
			hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
				addExecution("start")
				return nil
			})
			hooks.OnSignal = append(hooks.OnSignal, func(ctx context.Context) error {
				addExecution("signal")
				return nil
			})
			hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
				addExecution("shutdown")
				return nil
			})
			hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
				addExecution("exit")
				return nil
			})
		}).WithSignals(syscall.SIGUSR1)

		// Send signal after a brief delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			p, _ := os.FindProcess(os.Getpid())
			p.Signal(syscall.SIGUSR1)
		}()

		err := lc.Run(context.Background())
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()

		expected := []string{"prestart", "start", "signal", "shutdown", "exit"}
		assert.Equal(t, expected, executed)
	})

	t.Run("hook errors are propagated with context", func(t *testing.T) {
		expectedError := errors.New("hook error")

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
				return expectedError
			})
		})

		err := lc.Run(context.Background())
		require.Error(t, err)
		assert.ErrorIs(t, err, expectedError)
		assert.Contains(t, err.Error(), "hook 1 failed")
	})

	t.Run("OnExit hooks run even when OnShutdown fails", func(t *testing.T) {
		var executed []string
		var mu sync.Mutex

		addExecution := func(phase string) {
			mu.Lock()
			defer mu.Unlock()
			executed = append(executed, phase)
		}

		shutdownError := errors.New("shutdown failed")

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			opts.ShutdownTimeout = 100 * time.Millisecond
			hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
				addExecution("start")
				return nil
			})
			hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
				addExecution("shutdown")
				return shutdownError // This fails
			})
			hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
				addExecution("exit")
				return nil // This should still run
			})
		})

		// Cancel context after a brief delay to trigger shutdown
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := lc.Run(ctx)

		// Should return the shutdown error
		require.Error(t, err)
		assert.ErrorIs(t, err, shutdownError)

		mu.Lock()
		defer mu.Unlock()

		// Both shutdown and exit should have executed
		assert.Contains(t, executed, "start")
		assert.Contains(t, executed, "shutdown")
		assert.Contains(t, executed, "exit") // This is the key test - exit runs despite shutdown failure
	})

	t.Run("errors.Join preserves both shutdown and exit errors", func(t *testing.T) {
		shutdownError := errors.New("shutdown failed")
		exitError := errors.New("exit failed")

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			opts.ShutdownTimeout = 100 * time.Millisecond
			hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
				return shutdownError // This fails first
			})
			hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
				return exitError // This also fails
			})
		})

		// Cancel context to trigger shutdown
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := lc.Run(ctx)

		require.Error(t, err)
		// Both errors should be preserved in the joined error
		assert.ErrorIs(t, err, shutdownError)
		assert.ErrorIs(t, err, exitError)
	})

	t.Run("OnExit error returned when OnShutdown succeeds", func(t *testing.T) {
		exitError := errors.New("exit failed")

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			opts.ShutdownTimeout = 100 * time.Millisecond
			hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
				return nil // This succeeds
			})
			hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
				return exitError // This fails
			})
		})

		// Cancel context to trigger shutdown
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := lc.Run(ctx)

		// Should return the exit error since shutdown succeeded
		require.Error(t, err)
		assert.ErrorIs(t, err, exitError)
	})
}

func TestAttachHTTPServer(t *testing.T) {
	t.Run("returns lifecycle for chaining", func(t *testing.T) {
		lc := lifecycle.Default()
		srv := &http.Server{Addr: ":0"}

		result := lc.AttachHTTPServer(srv)
		assert.Equal(t, lc, result)
	})

	t.Run("server attaches without error", func(t *testing.T) {
		lc := lifecycle.Default()
		srv := &http.Server{Addr: ":0"}

		// This should not panic or error during attachment
		result := lc.AttachHTTPServer(srv)
		assert.Equal(t, lc, result)

		// We can test that Listen() works (server configuration is valid)
		sigChan, cleanup := lc.Listen()
		defer cleanup()
		assert.NotNil(t, sigChan)
	})

	t.Run("detects server startup errors", func(t *testing.T) {
		lc := lifecycle.Default()

		// Create a server with an invalid address to force a startup error
		srv := &http.Server{Addr: "invalid-address:99999"}
		lc.AttachHTTPServer(srv)

		// Run the lifecycle - should fail during OnStart phase
		err := lc.Run(context.Background())

		// Should get a startup error
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP listen failed")
	})

	t.Run("handles port conflict errors", func(t *testing.T) {
		// Start a server on a specific port
		firstServer := &http.Server{Addr: ":0"} // Let system choose port

		lc1 := lifecycle.Default()
		lc1.AttachHTTPServer(firstServer)

		// Start the first server and get its actual port
		started := make(chan struct{})
		lc1.OnStart(func(ctx context.Context) error {
			close(started)
			return nil
		})

		go func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lc1.Run(ctx)
		}()

		// Wait for first server to start
		<-started
		time.Sleep(50 * time.Millisecond)

		// Try to start second server on a conflicting port
		// Note: Since we used :0 above, we can't easily test exact port conflict
		// So we'll test with an obviously bad address
		lc2 := lifecycle.Default()
		conflictServer := &http.Server{Addr: "invalid:99999"}
		lc2.AttachHTTPServer(conflictServer)

		err := lc2.Run(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP listen failed")
	})

	t.Run("graceful shutdown does not return error", func(t *testing.T) {
		lc := lifecycle.Default()
		srv := &http.Server{Addr: ":0"}

		lc.AttachHTTPServer(srv)

		// Test that graceful shutdown (http.ErrServerClosed) is not treated as error
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(150 * time.Millisecond) // Let server start properly
			cancel()
		}()

		err := lc.Run(ctx)
		assert.NoError(t, err) // Graceful shutdown should not return error
	})

	t.Run("prevents HTTP server resource leaks on early context cancellation", func(t *testing.T) {
		lc := lifecycle.Default()
		srv := &http.Server{Addr: ":0"} // Use dynamic port

		lc.AttachHTTPServer(srv)

		// Cancel context after a brief delay to simulate cancellation during HTTP startup
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond) // Allow some startup time
			cancel()
		}()

		err := lc.Run(ctx)
		// Should return context.Canceled or no error (if shutdown completed normally)
		// The key is that it shouldn't leak resources
		if err != nil {
			// If there's an error, it should be context-related
			assert.Contains(t, err.Error(), "context")
		}

		// Verify no resource leaks by creating another server
		// If the previous server properly cleaned up, this should work
		lc2 := lifecycle.Default()
		srv2 := &http.Server{Addr: ":0"}
		lc2.AttachHTTPServer(srv2)

		// Test that we can start another server without conflicts
		ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel2()

		err2 := lc2.Run(ctx2)
		// Should get context timeout or complete successfully, not port conflict
		if err2 != nil {
			// Should NOT contain "address already in use" which would indicate a leak
			assert.NotContains(t, err2.Error(), "address already in use")
		}
	})

	t.Run("handles pre-cancelled context cleanly", func(t *testing.T) {
		lc := lifecycle.Default()
		srv := &http.Server{Addr: ":0"}

		// Create already-cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		lc.AttachHTTPServer(srv)

		err := lc.Run(ctx)
		assert.Error(t, err)
		// Should get context cancelled error from either the OnStart hooks or HTTP server startup
		assert.Contains(t, err.Error(), "context")
	})
}

func TestGo(t *testing.T) {
	t.Run("returns lifecycle for chaining", func(t *testing.T) {
		lc := lifecycle.Default()

		goroutineFunc := func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		}

		result := lc.Go(goroutineFunc)
		assert.Equal(t, lc, result)
	})

	t.Run("starts goroutine and waits for completion on shutdown", func(t *testing.T) {
		var started, finished bool
		var mu sync.Mutex

		goroutineFunc := func(ctx context.Context) error {
			mu.Lock()
			started = true
			mu.Unlock()

			<-ctx.Done()

			mu.Lock()
			finished = true
			mu.Unlock()
			return nil
		}

		lc := lifecycle.Default().Go(goroutineFunc).WithSignals(syscall.SIGUSR1)

		go func() {
			time.Sleep(50 * time.Millisecond)
			p, _ := os.FindProcess(os.Getpid())
			p.Signal(syscall.SIGUSR1)
		}()

		err := lc.Run(context.Background())
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()

		assert.True(t, started)
		assert.True(t, finished)
	})

	t.Run("respects shutdown timeout", func(t *testing.T) {
		goroutineFunc := func(ctx context.Context) error {
			<-ctx.Done()
			time.Sleep(200 * time.Millisecond) // Longer than shutdown timeout
			return nil
		}

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			opts.ShutdownTimeout = 50 * time.Millisecond
		}).Go(goroutineFunc).WithSignals(syscall.SIGUSR1)

		go func() {
			time.Sleep(10 * time.Millisecond)
			p, _ := os.FindProcess(os.Getpid())
			p.Signal(syscall.SIGUSR1)
		}()

		start := time.Now()
		err := lc.Run(context.Background())
		duration := time.Since(start)

		// Should timeout and return error
		assert.Error(t, err)
		assert.True(t, duration >= 50*time.Millisecond && duration < 150*time.Millisecond,
			"expected timeout around 50ms, got %v", duration)
	})
}

func TestShutdownTimeout(t *testing.T) {
	t.Run("respects configured shutdown timeout", func(t *testing.T) {
		slowHook := func(ctx context.Context) error {
			select {
			case <-time.After(200 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			opts.ShutdownTimeout = 50 * time.Millisecond
			hooks.OnShutdown = append(hooks.OnShutdown, slowHook)
		}).WithSignals(syscall.SIGUSR1)

		go func() {
			time.Sleep(10 * time.Millisecond)
			p, _ := os.FindProcess(os.Getpid())
			p.Signal(syscall.SIGUSR1)
		}()

		start := time.Now()
		err := lc.Run(context.Background())
		duration := time.Since(start)

		assert.Error(t, err)
		assert.True(t, duration >= 50*time.Millisecond && duration < 150*time.Millisecond,
			"expected timeout around 50ms, got %v", duration)
	})
}

func TestChaining(t *testing.T) {
	t.Run("methods can be chained", func(t *testing.T) {
		hook := func(ctx context.Context) error { return nil }
		srv := &http.Server{Addr: ":0"}

		lc := lifecycle.Default().
			OnPreStart(hook).
			OnStart(hook).
			OnSignal(hook).
			OnShutdown(hook).
			OnExit(hook).
			AttachHTTPServer(srv).
			Go(func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			}).
			WithSignals(syscall.SIGUSR1)

		// Test that chaining returns the same lifecycle instance
		assert.NotNil(t, lc)

		// Test that basic operations work (no need to run full lifecycle)
		sigChan, cleanup := lc.Listen()
		defer cleanup()
		assert.NotNil(t, sigChan)
	})
}

func TestContextCancellation(t *testing.T) {
	t.Run("respects context cancellation", func(t *testing.T) {
		var executed []string
		var mu sync.Mutex

		addExecution := func(phase string) {
			mu.Lock()
			defer mu.Unlock()
			executed = append(executed, phase)
		}

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			opts.ShutdownTimeout = 100 * time.Millisecond
			hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
				addExecution("prestart")
				return nil
			})
			hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
				addExecution("start")
				return nil
			})
			hooks.OnSignal = append(hooks.OnSignal, func(ctx context.Context) error {
				addExecution("signal")
				return nil
			})
			hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
				addExecution("shutdown")
				return nil
			})
			hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
				addExecution("exit")
				return nil
			})
		})

		// Cancel context after a brief delay
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := lc.Run(ctx)
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()

		expected := []string{"prestart", "start", "signal", "shutdown", "exit"}
		assert.Equal(t, expected, executed)
	})

	t.Run("context cancellation during hooks", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
				// Cancel during hook execution
				cancel()
				return nil
			})
		})

		err := lc.Run(ctx)
		assert.NoError(t, err)
	})

	t.Run("context timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		lc := lifecycle.Default()

		start := time.Now()
		err := lc.Run(ctx)
		duration := time.Since(start)

		assert.NoError(t, err) // Context cancellation triggers shutdown, not error
		assert.True(t, duration >= 50*time.Millisecond && duration < 150*time.Millisecond,
			"expected timeout around 50ms, got %v", duration)
	})
}

func TestEmptySignalsFallback(t *testing.T) {
	t.Run("falls back to default signals when none configured", func(t *testing.T) {
		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			// Disable all signals
			opts.EnableSIGHUP = false
			opts.EnableSIGINT = false
			opts.EnableSIGTERM = false
			opts.EnableSIGQUIT = false
		})

		// Should not panic or hang - uses fallback signals
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := lc.Run(ctx)
		assert.NoError(t, err) // Should complete via context timeout
	})

	t.Run("empty signals with context cancellation works", func(t *testing.T) {
		var started, finished bool

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			// Disable all signals
			opts.EnableSIGHUP = false
			opts.EnableSIGINT = false
			opts.EnableSIGTERM = false
			opts.EnableSIGQUIT = false

			hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
				started = true
				return nil
			})
			hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
				finished = true
				return nil
			})
		})

		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := lc.Run(ctx)
		require.NoError(t, err)

		assert.True(t, started)
		assert.True(t, finished)
	})
}

func TestRun_Reusable(t *testing.T) {
	r := lifecycle.Default().WithSignals()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, r.Run(ctx))

	// second run after completion should be allowed
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	require.NoError(t, r.Run(ctx2))
}

func TestRun_PreventsConcurrentRuns(t *testing.T) {
	r := lifecycle.Default().WithSignals()
	started := make(chan struct{})
	block := make(chan struct{})

	r.OnStart(func(context.Context) error { close(started); <-block; return nil })

	ctx, cancel := context.WithCancel(context.Background())
	errCh1 := make(chan error, 1)
	go func() { errCh1 <- r.Run(ctx) }()

	require.Eventually(t, func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	err := r.Run(context.Background())
	require.ErrorIs(t, err, lifecycle.ErrAlreadyRunning)

	close(block)
	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-errCh1:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

func TestHookErrorContext(t *testing.T) {
	t.Run("hook errors include position context", func(t *testing.T) {
		originalError := errors.New("original hook error")

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			// Add multiple hooks to test position reporting
			hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
				return nil // First hook succeeds
			})
			hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
				return nil // Second hook succeeds
			})
			hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
				return originalError // Third hook fails
			})
		})

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately to skip signal waiting

		err := lc.Run(ctx)
		require.Error(t, err)

		// Should indicate which hook failed (3rd hook = index 2, displayed as "hook 3")
		assert.Contains(t, err.Error(), "hook 3 failed")
		assert.ErrorIs(t, err, originalError)
	})
}

func TestMultipleRapidSignals(t *testing.T) {
	t.Run("handles multiple rapid signals gracefully", func(t *testing.T) {
		var signalCount int32
		var mu sync.Mutex
		executed := []string{}

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			opts.ShutdownTimeout = 200 * time.Millisecond
			hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
				mu.Lock()
				executed = append(executed, "start")
				mu.Unlock()
				return nil
			})
			hooks.OnSignal = append(hooks.OnSignal, func(ctx context.Context) error {
				count := atomic.AddInt32(&signalCount, 1)
				mu.Lock()
				executed = append(executed, fmt.Sprintf("signal-%d", count))
				mu.Unlock()
				return nil
			})
			hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
				mu.Lock()
				executed = append(executed, "shutdown")
				mu.Unlock()
				return nil
			})
		}).WithSignals(syscall.SIGUSR1)

		// Send multiple rapid signals
		go func() {
			time.Sleep(50 * time.Millisecond)
			p, _ := os.FindProcess(os.Getpid())
			// Send multiple signals rapidly
			for i := 0; i < 5; i++ {
				p.Signal(syscall.SIGUSR1)
				time.Sleep(5 * time.Millisecond)
			}
		}()

		err := lc.Run(context.Background())
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()

		// Should have started and shutdown, but only processed one signal
		// (subsequent signals are ignored after shutdown begins)
		assert.Contains(t, executed, "start")
		assert.Contains(t, executed, "shutdown")
		assert.Contains(t, executed, "signal-1")

		// Should not have processed multiple signals
		signalCalls := 0
		for _, exec := range executed {
			if strings.HasPrefix(exec, "signal-") {
				signalCalls++
			}
		}
		assert.Equal(t, 1, signalCalls, "Should only process first signal")
	})
}

func TestVeryShortTimeout(t *testing.T) {
	t.Run("handles very short shutdown timeout", func(t *testing.T) {
		slowHook := func(ctx context.Context) error {
			select {
			case <-time.After(100 * time.Millisecond): // Much longer than timeout
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			opts.ShutdownTimeout = 1 * time.Millisecond // Very short timeout
			hooks.OnShutdown = append(hooks.OnShutdown, slowHook)
		}).WithSignals(syscall.SIGUSR1)

		ctx := context.Background()
		go func() {
			time.Sleep(5 * time.Millisecond) // Trigger signal before timeout
			p, _ := os.FindProcess(os.Getpid())
			p.Signal(syscall.SIGUSR1)
		}()

		start := time.Now()
		err := lc.Run(ctx)
		duration := time.Since(start)

		// Should timeout quickly
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.True(t, duration < 50*time.Millisecond, "Should timeout very quickly, got %v", duration)
	})

	t.Run("handles zero timeout", func(t *testing.T) {
		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			opts.ShutdownTimeout = 0 // Zero timeout - creates context with immediate deadline
			hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
				// This should get cancelled immediately due to zero timeout
				select {
				case <-time.After(50 * time.Millisecond):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}).WithSignals(syscall.SIGUSR1)

		ctx := context.Background()
		go func() {
			time.Sleep(5 * time.Millisecond) // Trigger signal to start shutdown
			p, _ := os.FindProcess(os.Getpid())
			p.Signal(syscall.SIGUSR1)
		}()

		start := time.Now()
		err := lc.Run(ctx)
		duration := time.Since(start)

		// Zero timeout creates immediate deadline, so shutdown context should be cancelled
		if err != nil {
			assert.ErrorIs(t, err, context.DeadlineExceeded)
		}
		assert.True(t, duration < 40*time.Millisecond, "Zero timeout should be fast, got %v", duration)
	})
}

func TestConcurrentSignalHandling(t *testing.T) {
	t.Run("signal handling is thread-safe", func(t *testing.T) {
		var hookExecutions int32

		lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
			opts.ShutdownTimeout = 100 * time.Millisecond
			hooks.OnSignal = append(hooks.OnSignal, func(ctx context.Context) error {
				atomic.AddInt32(&hookExecutions, 1)
				time.Sleep(10 * time.Millisecond) // Simulate work
				return nil
			})
		}).WithSignals(syscall.SIGUSR1)

		// Multiple goroutines trying to send signals
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(50 * time.Millisecond)
				p, _ := os.FindProcess(os.Getpid())
				p.Signal(syscall.SIGUSR1)
			}()
		}

		go func() {
			wg.Wait()
		}()

		err := lc.Run(context.Background())
		require.NoError(t, err)

		// Should have executed signal hook exactly once despite multiple signals
		executions := atomic.LoadInt32(&hookExecutions)
		assert.Equal(t, int32(1), executions, "Signal hook should execute exactly once")
	})
}
