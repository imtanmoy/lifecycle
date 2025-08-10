#!/bin/bash

# Quick Start Script for Lifecycle Examples
# This script helps you quickly test each example

set -e  # Exit on any error

echo "🌟 Lifecycle Examples Quick Start"
echo "=================================="
echo

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.21 or later."
    exit 1
fi

echo "✅ Go version: $(go version)"
echo

# Function to run an example
run_example() {
    local example_name="$1"
    local example_dir="$2"
    local description="$3"
    
    echo "🚀 Running: $example_name"
    echo "📝 Description: $description"
    echo "📁 Directory: $example_dir"
    echo
    
    cd "$example_dir"
    
    # Install dependencies
    echo "📦 Installing dependencies..."
    go mod tidy
    
    echo "▶️  Starting $example_name..."
    echo "💡 Press Ctrl+C to stop and see graceful shutdown"
    echo "🌐 Check the logs for HTTP endpoints to test"
    echo
    
    # Run the example
    go run main.go
}

# Function to setup CLI tool test data
setup_cli_tool() {
    echo "📁 Setting up CLI tool test data..."
    cd cli-tool
    mkdir -p input output
    echo "Hello World - File 1" > input/file1.txt
    echo "This is a test file" > input/file2.txt  
    echo "Another file to process" > input/file3.txt
    echo "✅ Created 3 test files in input/"
    cd ..
}

# Main menu
echo "Choose an example to run:"
echo
echo "1) 🌐 Web Server - HTTP API with middleware and stats"
echo "2) 🏗️  Microservice - Complete service with database and workers" 
echo "3) ⚡ CLI Tool - File processor with progress tracking"
echo "4) 👷 Worker Pool - Job processing with HTTP management"
echo "5) 📊 Monitoring - Metrics collection and alerting"
echo "6) 🧪 Test CLI Tool Setup - Just setup test files"
echo "7) ❓ Show detailed instructions"
echo
read -p "Enter your choice (1-7): " choice

case $choice in
    1)
        run_example "Web Server" "web-server" "Production HTTP API server"
        ;;
    2)
        run_example "Microservice" "microservice" "Complete microservice with database"
        ;;
    3)
        setup_cli_tool
        echo
        echo "🏃 Running CLI Tool with: ./input -> ./output"
        run_example "CLI Tool" "cli-tool" "Batch file processor"
        ;;
    4)
        run_example "Worker Pool" "worker-pool" "Distributed job processing"
        ;;
    5)
        run_example "Monitoring Service" "monitoring" "Metrics collection and alerting"
        ;;
    6)
        setup_cli_tool
        echo "✅ CLI tool test data setup complete!"
        echo "   Now run: cd cli-tool && go run main.go ./input ./output"
        ;;
    7)
        echo
        echo "📖 Detailed Instructions:"
        echo "========================"
        echo
        echo "Each example demonstrates different lifecycle patterns:"
        echo
        echo "🌐 Web Server (Port 8080):"
        echo "   - HTTP endpoints with middleware"
        echo "   - Test: curl http://localhost:8080/health"
        echo
        echo "🏗️ Microservice (Port 8080):"
        echo "   - Database + cache + background workers"
        echo "   - Test: curl http://localhost:8080/users"
        echo
        echo "⚡ CLI Tool:"
        echo "   - Processes files from input/ to output/"
        echo "   - Try interrupting with Ctrl+C and resuming"
        echo
        echo "👷 Worker Pool (Port 8080):"
        echo "   - Job processing with HTTP API"
        echo "   - Test: curl http://localhost:8080/stats"
        echo
        echo "📊 Monitoring (Port 9090):"
        echo "   - Metrics collection with Prometheus format"
        echo "   - Test: curl http://localhost:9090/metrics"
        echo
        echo "💡 Tips:"
        echo "   - Watch the startup/shutdown logs carefully"
        echo "   - Try interrupting with Ctrl+C to see graceful shutdown"
        echo "   - HTTP examples can be tested with curl or browser"
        echo "   - Only run one HTTP example at a time (port conflicts)"
        echo
        ;;
    *)
        echo "❌ Invalid choice. Please run the script again."
        exit 1
        ;;
esac