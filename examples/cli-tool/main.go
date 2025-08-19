package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/imtanmoy/lifecycle"
)

// FileProcessor handles batch file processing with progress tracking
type FileProcessor struct {
	inputDir     string
	outputDir    string
	progressFile string

	// Processing stats
	totalFiles     int64
	processedFiles int64
	errorFiles     int64

	// Progress tracking
	processedList []string
	mu            sync.Mutex

	// Cleanup tracking
	tempFiles []string
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run main.go <input-dir> <output-dir>")
		fmt.Println("Example: go run main.go ./input ./output")
		os.Exit(1)
	}

	inputDir := os.Args[1]
	outputDir := os.Args[2]

	processor := &FileProcessor{
		inputDir:      inputDir,
		outputDir:     outputDir,
		progressFile:  filepath.Join(outputDir, ".progress"),
		processedList: make([]string, 0),
		tempFiles:     make([]string, 0),
	}

	// Create lifecycle for CLI tool
	lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
		opts.ShutdownTimeout = 15 * time.Second

		// === PRE-START: Validate arguments and setup ===
		hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
			log.Println("🔍 Validating input parameters...")

			// Check if input directory exists
			if _, err := os.Stat(inputDir); os.IsNotExist(err) {
				return fmt.Errorf("input directory does not exist: %s", inputDir)
			}

			// Create output directory if it doesn't exist
			if err := os.MkdirAll(outputDir, 0755); err != nil {
				return fmt.Errorf("failed to create output directory: %w", err)
			}

			log.Printf("✅ Input: %s, Output: %s", inputDir, outputDir)
			return nil
		})

		// === START: Initialize processing ===
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("🚀 Initializing file processor...")

			// Scan input directory for files
			err := processor.scanInputFiles()
			if err != nil {
				return fmt.Errorf("failed to scan input files: %w", err)
			}

			// Load previous progress if exists
			processor.loadProgress()

			log.Printf("📁 Found %d files to process", atomic.LoadInt64(&processor.totalFiles))
			if len(processor.processedList) > 0 {
				log.Printf("📋 Resuming from previous run (%d already processed)", len(processor.processedList))
			}

			return nil
		})

		// === MAIN PROCESSING ===
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("⚡ Starting file processing...")
			return processor.processFiles(ctx)
		})

		// === SIGNAL HANDLING ===
		hooks.OnSignal = append(hooks.OnSignal, func(ctx context.Context) error {
			processed := atomic.LoadInt64(&processor.processedFiles)
			total := atomic.LoadInt64(&processor.totalFiles)
			errors := atomic.LoadInt64(&processor.errorFiles)

			log.Println("📡 Interruption signal received!")
			log.Printf("📊 Progress: %d/%d files processed, %d errors", processed, total, errors)
			log.Println("💾 Saving progress for resume...")

			return nil
		})

		// === SHUTDOWN: Save progress and cleanup ===
		hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
			log.Println("💾 Saving processing progress...")

			if err := processor.saveProgress(); err != nil {
				log.Printf("⚠️  Failed to save progress: %v", err)
				return err
			}

			log.Println("✅ Progress saved successfully")
			return nil
		})

		// === EXIT: Final cleanup ===
		hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
			log.Println("🧹 Cleaning up temporary files...")

			// Clean up temporary files
			for _, tempFile := range processor.tempFiles {
				if err := os.Remove(tempFile); err != nil {
					log.Printf("⚠️  Failed to remove temp file %s: %v", tempFile, err)
				}
			}

			// Final statistics
			processed := atomic.LoadInt64(&processor.processedFiles)
			total := atomic.LoadInt64(&processor.totalFiles)
			errors := atomic.LoadInt64(&processor.errorFiles)

			log.Println("📊 Final Statistics:")
			log.Printf("   Total files: %d", total)
			log.Printf("   Processed: %d", processed)
			log.Printf("   Errors: %d", errors)

			if processed == total && errors == 0 {
				log.Println("✨ All files processed successfully!")
				// Remove progress file on successful completion
				os.Remove(processor.progressFile)
			} else {
				log.Println("⚠️  Processing incomplete - progress saved for resume")
			}

			return nil
		})
	})

	// Start the CLI tool
	log.Println("🌟 File Processing CLI Tool")
	log.Printf("📂 Processing files from %s to %s", inputDir, outputDir)
	log.Println("💡 Press Ctrl+C to safely interrupt and save progress")

	if err := lc.Run(context.Background()); err != nil {
		log.Fatalf("❌ CLI tool failed: %v", err)
	}
}

// scanInputFiles discovers all files to process
func (fp *FileProcessor) scanInputFiles() error {
	var count int64

	err := filepath.Walk(fp.inputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only process regular files (not directories)
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".txt") {
			count++
		}

		return nil
	})

	atomic.StoreInt64(&fp.totalFiles, count)
	return err
}

// processFiles performs the actual file processing
func (fp *FileProcessor) processFiles(ctx context.Context) error {
	return filepath.Walk(fp.inputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-txt files
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".txt") {
			return nil
		}

		// Check if already processed
		fp.mu.Lock()
		alreadyProcessed := fp.contains(fp.processedList, path)
		fp.mu.Unlock()

		if alreadyProcessed {
			return nil
		}

		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Process the file
		if err := fp.processFile(path); err != nil {
			log.Printf("❌ Error processing %s: %v", path, err)
			atomic.AddInt64(&fp.errorFiles, 1)
		} else {
			// Mark as processed
			fp.mu.Lock()
			fp.processedList = append(fp.processedList, path)
			fp.mu.Unlock()

			atomic.AddInt64(&fp.processedFiles, 1)

			processed := atomic.LoadInt64(&fp.processedFiles)
			total := atomic.LoadInt64(&fp.totalFiles)
			progress := float64(processed) / float64(total) * 100

			log.Printf("✅ [%.1f%%] Processed: %s", progress, filepath.Base(path))
		}

		// Simulate processing time
		time.Sleep(200 * time.Millisecond)

		return nil
	})
}

// processFile simulates file processing (uppercase conversion)
func (fp *FileProcessor) processFile(inputPath string) error {
	// Read input file
	content, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Create temporary file for processing
	tempFile := filepath.Join(fp.outputDir, ".temp_"+filepath.Base(inputPath))
	fp.tempFiles = append(fp.tempFiles, tempFile)

	// Process content (convert to uppercase)
	processedContent := strings.ToUpper(string(content))

	// Write to temporary file first
	if err := os.WriteFile(tempFile, []byte(processedContent), 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Move temp file to final location
	outputPath := filepath.Join(fp.outputDir, filepath.Base(inputPath))
	if err := os.Rename(tempFile, outputPath); err != nil {
		return fmt.Errorf("failed to move processed file: %w", err)
	}

	// Remove from temp files list (successfully processed)
	fp.tempFiles = fp.removeFromSlice(fp.tempFiles, tempFile)

	return nil
}

// Progress management
func (fp *FileProcessor) loadProgress() {
	if content, err := os.ReadFile(fp.progressFile); err == nil {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				fp.processedList = append(fp.processedList, line)
			}
		}
	}
}

func (fp *FileProcessor) saveProgress() error {
	fp.mu.Lock()
	defer fp.mu.Unlock()

	content := strings.Join(fp.processedList, "\n")
	return os.WriteFile(fp.progressFile, []byte(content), 0644)
}

// Helper functions
func (fp *FileProcessor) contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (fp *FileProcessor) removeFromSlice(slice []string, item string) []string {
	for i, s := range slice {
		if s == item {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
