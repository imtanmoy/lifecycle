# Lifecycle

[![CI](https://github.com/imtanmoy/lifecycle/actions/workflows/ci.yml/badge.svg)](https://github.com/imtanmoy/lifecycle/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/imtanmoy/lifecycle)](https://goreportcard.com/report/github.com/imtanmoy/lifecycle)
[![Go Reference](https://pkg.go.dev/badge/github.com/imtanmoy/lifecycle.svg)](https://pkg.go.dev/github.com/imtanmoy/lifecycle)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A Go library for managing application lifecycle with structured hooks, graceful shutdown, and customizable signal handling.

## Features

- 🔄 **Structured Lifecycle Hooks**: PreStart → Start → Signal → Shutdown → Exit
- 🛡️ **Graceful Shutdown**: Configurable timeout with proper resource cleanup
- 📡 **Signal Handling**: Cross-platform support (Unix/Windows) with customizable signals
- 🌐 **HTTP Server Integration**: Attach and manage `net/http.Server` automatically
- 🏃 **Goroutine Management**: Automatic goroutine lifecycle tracking
- 🔗 **Fluent API**: Chain methods for clean configuration
- 🚫 **Concurrent Protection**: Prevents multiple concurrent lifecycle runs
- 🔀 **Context Cancellation**: Full context.Context support throughout
- ⚡ **Resource Safety**: Prevents leaks with proper cleanup

## Installation

```bash
go get github.com/imtanmoy/lifecycle
```

## Lifecycle Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                     Application Lifecycle                       │
└─────────────────────────────────────────────────────────────────┘

    🚀 Application Start
            │
            ▼
    ┌───────────────┐
    │  OnPreStart   │ ◄── Configuration validation
    │    Hooks      │     Dependency injection
    └───────────────┘     Database connections
            │
            ▼
    ┌───────────────┐
    │   OnStart     │ ◄── Server startup
    │    Hooks      │     Service initialization
    └───────────────┘     Background workers
            │
            ▼
    ┌───────────────┐
    │ Wait for      │ ◄── SIGINT, SIGTERM, SIGHUP, SIGQUIT
    │ Signal/Cancel │     Context cancellation
    └───────────────┘     Manual shutdown
            │
            ▼
    ┌───────────────┐
    │  OnSignal     │ ◄── Signal received notification
    │    Hooks      │     Cleanup preparation
    └───────────────┘
            │
            ▼
    ┌───────────────┐
    │ OnShutdown    │ ◄── Graceful shutdown (with timeout)
    │    Hooks      │     HTTP server shutdown
    └───────────────┘     Connection draining
            │
            ▼
    ┌───────────────┐     ◄── ⚠️  ALWAYS RUNS (even if shutdown fails)
    │   OnExit      │ ◄── Final cleanup
    │    Hooks      │     Resource deallocation
    └───────────────┘     Log finalization
            │
            ▼
    🏁 Application End

┌─────────────────────────────────────────────────────────────────┐
│                         Key Guarantees                          │
│                                                                 │
│ ✅ OnExit hooks ALWAYS execute - Even if OnShutdown fails      │
│ ⏱️  Shutdown timeout enforcement - Operations respect timeouts  │
│ 🔗 Error preservation - Both errors preserved via errors.Join() │
│ 🛡️  Resource safety - Proper cleanup on early cancellation     │
│ 🔒 Thread-safe execution - Safe concurrent signal handling     │
└─────────────────────────────────────────────────────────────────┘
```

### Key Guarantees

- ✅ **OnExit hooks ALWAYS execute** - Even if OnShutdown fails
- ⏱️ **Shutdown timeout enforcement** - Operations respect configured timeouts  
- 🔗 **Error preservation** - Both shutdown and exit errors preserved via `errors.Join()`
- 🛡️ **Resource safety** - Proper cleanup on early cancellation
- 🔒 **Thread-safe execution** - Safe concurrent signal handling

## Quick Start

```go
package main

import (
    "context"
    "log"
    "time"
    
    "github.com/imtanmoy/lifecycle"
)

func main() {
    // Create lifecycle with custom configuration
    lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
        opts.ShutdownTimeout = 30 * time.Second
        
        hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
            log.Println("Validating configuration...")
            return nil
        })
        
        hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
            log.Println("Application started")
            return nil
        })
        
        hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
            log.Println("Shutting down gracefully...")
            return nil
        })
        
        hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
            log.Println("Cleanup completed")
            return nil
        })
    })
    
    // Start the lifecycle
    if err := lc.Run(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

## Usage Examples

### HTTP Server Integration

```go
package main

import (
    "context"
    "log"
    "net/http"
    
    "github.com/imtanmoy/lifecycle"
)

func main() {
    // Create HTTP server
    mux := http.NewServeMux()
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Hello, World!"))
    })
    
    server := &http.Server{
        Addr:    ":8080",
        Handler: mux,
    }
    
    // Attach server to lifecycle
    lc := lifecycle.Default().AttachHTTPServer(server)
    
    if err := lc.Run(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

### Goroutine Management

```go
package main

import (
    "context"
    "log"
    "time"
    
    "github.com/imtanmoy/lifecycle"
)

func main() {
    lc := lifecycle.Default().Go(func(ctx context.Context) error {
        // Long-running goroutine
        ticker := time.NewTicker(5 * time.Second)
        defer ticker.Stop()
        
        for {
            select {
            case <-ticker.C:
                log.Println("Background task running...")
            case <-ctx.Done():
                log.Println("Background task stopping...")
                return ctx.Err()
            }
        }
    })
    
    if err := lc.Run(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

### Custom Signal Handling

```go
package main

import (
    "context"
    "log"
    "syscall"
    "time"
    
    "github.com/imtanmoy/lifecycle"
)

func main() {
    lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
        // Custom shutdown timeout
        opts.ShutdownTimeout = 10 * time.Second
        
        // Disable specific signals
        opts.EnableSIGHUP = false
        opts.EnableSIGQUIT = false
    }).WithSignals(syscall.SIGINT, syscall.SIGTERM) // Custom signals
    
    if err := lc.Run(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

### Database Connection Management

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "log"
    
    "github.com/imtanmoy/lifecycle"
    _ "github.com/lib/pq" // postgres driver
)

func main() {
    var db *sql.DB
    
    lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
        hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
            // Initialize database connection
            var err error
            db, err = sql.Open("postgres", "connection-string")
            if err != nil {
                return fmt.Errorf("failed to open database: %w", err)
            }
            
            // Test connection
            if err := db.PingContext(ctx); err != nil {
                return fmt.Errorf("failed to ping database: %w", err)
            }
            
            log.Println("Database connected")
            return nil
        })
        
        hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
            if db != nil {
                if err := db.Close(); err != nil {
                    return fmt.Errorf("failed to close database: %w", err)
                }
                log.Println("Database disconnected")
            }
            return nil
        })
    })
    
    if err := lc.Run(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

### Multiple Services

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net"
    "net/http"
    "time"
    
    "github.com/imtanmoy/lifecycle"
    "google.golang.org/grpc"
)

func main() {
    // HTTP server
    httpServer := &http.Server{Addr: ":8080", Handler: http.NewServeMux()}
    
    // gRPC server
    grpcServer := grpc.NewServer()
    
    lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
        opts.ShutdownTimeout = 30 * time.Second
    }).
    AttachHTTPServer(httpServer).
    OnStart(func(ctx context.Context) error {
        // Start gRPC server
        listener, err := net.Listen("tcp", ":9090")
        if err != nil {
            return fmt.Errorf("failed to listen on :9090: %w", err)
        }
        
        go func() {
            if err := grpcServer.Serve(listener); err != nil {
                log.Printf("gRPC server error: %v", err)
            }
        }()
        
        log.Println("gRPC server started on :9090")
        return nil
    }).
    OnShutdown(func(ctx context.Context) error {
        // Graceful stop gRPC server
        grpcServer.GracefulStop()
        log.Println("gRPC server stopped")
        return nil
    }).
    Go(func(ctx context.Context) error {
        // Background worker
        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()
        
        for {
            select {
            case <-ticker.C:
                log.Println("Performing background maintenance...")
            case <-ctx.Done():
                return ctx.Err()
            }
        }
    })
    
    if err := lc.Run(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

## Configuration Options

```go
type Options struct {
    ShutdownTimeout time.Duration // Maximum time to wait for graceful shutdown (default: 30s)
    EnableSIGHUP    bool         // Listen for SIGHUP signals (default: true)
    EnableSIGINT    bool         // Listen for SIGINT signals (default: true)
    EnableSIGTERM   bool         // Listen for SIGTERM signals (default: true)
    EnableSIGQUIT   bool         // Listen for SIGQUIT signals (default: true)
}
```

## Error Handling

The lifecycle uses Go 1.20's `errors.Join()` to preserve multiple errors when both OnShutdown and OnExit hooks fail:

```go
package main

import (
    "context"
    "errors"
    "log"
    "time"
    
    "github.com/imtanmoy/lifecycle"
)

var (
    shutdownErr = errors.New("database shutdown failed")
    cleanupErr  = errors.New("cache cleanup failed")
)

func main() {
    lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
        hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
            return shutdownErr
        })
        hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
            return cleanupErr
        })
    })

    // Trigger shutdown immediately for demo
    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(100 * time.Millisecond)
        cancel()
    }()

    // Simple error inspection
    if err := lc.Run(ctx); err != nil {
        // Check specific errors
        if errors.Is(err, shutdownErr) {
            log.Println("Shutdown failed")
        }
        if errors.Is(err, cleanupErr) {
            log.Println("Cleanup failed") 
        }
        
        // Or just log the combined error
        log.Printf("Lifecycle error: %v", err)
        // Output: "database shutdown failed\ncache cleanup failed"
    }
}
```

## Advanced Features

### Context with Timeout

```go
package main

import (
    "context"
    "errors"
    "log"
    "time"
    
    "github.com/imtanmoy/lifecycle"
)

func main() {
    // Run with overall timeout
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    
    lc := lifecycle.Default()
    if err := lc.Run(ctx); err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            log.Println("Application timed out")
        }
    }
}
```

### Reusable Lifecycle

```go
package main

import (
    "context"
    
    "github.com/imtanmoy/lifecycle"
)

func main() {
    lc := lifecycle.Default()
    
    // First run
    ctx1, cancel1 := context.WithCancel(context.Background())
    cancel1() // Cancel immediately
    lc.Run(ctx1)
    
    // Second run (allowed after first completes)
    ctx2, cancel2 := context.WithCancel(context.Background())
    defer cancel2()
    lc.Run(ctx2)
}
```

### Signal Customization

```go
package main

import (
    "os"
    "syscall"
    
    "github.com/imtanmoy/lifecycle"
)

func main() {
    // Unix-specific signals
    lc := lifecycle.Default().WithSignals(
        syscall.SIGINT,
        syscall.SIGTERM,
        syscall.SIGUSR1, // Custom signal
    )
    
    // Windows-compatible alternative
    lc2 := lifecycle.Default().WithSignals(os.Interrupt)
    
    // Use one of them
    _ = lc
    _ = lc2
}
```

## Best Practices

### 1. **Resource Management**

```go
hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
    // Initialize resources
    return initializeResources()
})

hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
    // Always cleanup, even if shutdown failed
    return cleanup()
})
```

### 2. **Error Handling**

```go
hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
    if err := startService(); err != nil {
        return fmt.Errorf("failed to start service: %w", err)
    }
    return nil
})
```

### 3. **Context Awareness**

```go
hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
    // Respect shutdown timeout
    select {
    case <-doGracefulShutdown():
        return nil
    case <-ctx.Done():
        return ctx.Err() // Timeout or cancellation
    }
})
```

### 4. **Multiple Hook Registration**

```go
lc.OnStart(
    initializeCache,
    startWebServer,
    startMetricsServer,
).OnShutdown(
    stopMetricsServer,
    stopWebServer,
    shutdownCache,
)
```

## Platform Support

- **Unix/Linux**: Full signal support (SIGHUP, SIGINT, SIGTERM, SIGQUIT)
- **Windows**: `os.Interrupt` support
- **Cross-platform**: Automatic platform detection

## Testing

The package includes comprehensive tests covering:

- Signal handling scenarios
- Timeout edge cases  
- Resource leak prevention
- Concurrent execution protection
- Error preservation and propagation
- Hook execution order verification

Run tests:

```bash
go test -v
```

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
