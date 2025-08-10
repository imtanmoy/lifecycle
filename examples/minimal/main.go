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