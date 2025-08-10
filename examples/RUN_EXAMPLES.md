# How to Run the Lifecycle Examples

This guide shows you exactly how to run each example with step-by-step instructions, what to expect, and how to test them.

## Prerequisites

Make sure you have Go 1.21 or later installed:

```bash
go version
```

## Running Examples

### 1. Web Server Example 🌐

**Purpose**: Production HTTP API server with middleware and statistics

```bash
# Navigate to the example
cd examples/web-server

# Install dependencies and run
go mod tidy
go run main.go
```

**Expected output**:
```
🌟 Web Server Example
🔍 Validating server configuration...
✅ Configuration valid - listening on :8080
🚀 Starting web server...
🌐 Server starting on http://localhost:8080
📋 Available endpoints:
   GET /health - Health check
   GET /api/status - Server status
   GET /api/users - Mock users API
💡 Press Ctrl+C to gracefully shutdown
```

**Test the server** (in another terminal):
```bash
# Health check
curl http://localhost:8080/health

# Server status with statistics
curl http://localhost:8080/api/status

# Mock users API
curl http://localhost:8080/api/users

# View in browser
open http://localhost:8080/health
```

**To stop**: Press `Ctrl+C` and watch the graceful shutdown process.

---

### 2. Microservice Example 🏗️

**Purpose**: Complete microservice with database, cache, and background workers

```bash
# Navigate to the example
cd examples/microservice

# Install dependencies and run
go mod tidy
go run main.go
```

**Expected output**:
```
🌟 User Microservice Starting...
🔍 Pre-start: Validating environment...
🗄️  Initializing database connection...
✅ Database initialized successfully
🧠 Initializing cache...
✅ Cache initialized successfully
🔄 Starting background workers...
✅ Cache cleanup worker started
✅ Metrics collection worker started
✅ Health check worker started
🌐 HTTP API: http://localhost:8080
```

**Test the microservice**:
```bash
# Health check
curl http://localhost:8080/health

# Get all users (with caching)
curl http://localhost:8080/users

# Get specific user
curl http://localhost:8080/users/1

# Cache statistics
curl http://localhost:8080/cache/stats
```

**To stop**: Press `Ctrl+C` to see coordinated shutdown of all services.

---

### 3. CLI Tool Example ⚡

**Purpose**: Batch file processor with progress tracking and resumable operations

```bash
# Navigate to the example
cd examples/cli-tool

# Create test data
mkdir -p input output
echo "hello world" > input/file1.txt
echo "this is a test file" > input/file2.txt
echo "another file to process" > input/file3.txt

# Install dependencies and run
go mod tidy
go run main.go ./input ./output
```

**Expected output**:
```
🌟 File Processing CLI Tool
🔍 Validating input parameters...
✅ Input: ./input, Output: ./output
🚀 Initializing file processor...
📁 Found 3 files to process
⚡ Starting file processing...
✅ [33.3%] Processed: file1.txt
✅ [66.7%] Processed: file2.txt
✅ [100.0%] Processed: file3.txt
📊 Final Statistics:
   Total files: 3
   Processed: 3
   Errors: 0
✨ All files processed successfully!
```

**Test interruption** (to see progress saving):
```bash
# Start processing, then press Ctrl+C quickly
go run main.go ./input ./output
# Press Ctrl+C during processing

# Run again to see resume functionality
go run main.go ./input ./output
```

**Check results**:
```bash
# View processed files (converted to uppercase)
cat output/file1.txt
cat output/file2.txt
cat output/file3.txt
```

---

### 4. Worker Pool Example 👷

**Purpose**: Distributed job processing with HTTP management API

```bash
# Navigate to the example
cd examples/worker-pool

# Install dependencies and run
go mod tidy
go run main.go
```

**Expected output**:
```
🌟 Worker Pool Service Starting...
🔍 Initializing worker pool with 5 workers...
👷 Starting worker pool...
✅ Started 5 workers
📊 Starting result processor...
🎲 Starting demo job generator...
📈 Starting monitoring...
🌐 Management API: http://localhost:8080
👷 Worker pool: 5 workers, queue capacity: 100
💡 Press Ctrl+C to gracefully shutdown
```

**Test the worker pool**:
```bash
# Check pool status
curl http://localhost:8080/status

# View processing statistics
curl http://localhost:8080/stats

# Submit a custom job
curl -X POST http://localhost:8080/submit \
  -H "Content-Type: application/json" \
  -d '{"type":"test","payload":{"message":"Custom job"},"duration_ms":2000}'

# Health check
curl http://localhost:8080/health
```

**Watch the logs** to see:
- Demo jobs being automatically generated every 3 seconds
- Workers processing jobs
- Statistics being reported every 30 seconds

---

### 5. Monitoring Service Example 📊

**Purpose**: Metrics collection and alerting with Prometheus compatibility

```bash
# Navigate to the example
cd examples/monitoring

# Install dependencies and run
go mod tidy
go run main.go
```

**Expected output**:
```
🌟 Monitoring Service Starting...
🔍 Initializing monitoring service...
✅ Configured 3 alert rules
📊 Starting metric collectors...
✅ Started system collector (interval: 10s)
✅ Started application collector (interval: 15s)
✅ Started business collector (interval: 30s)
🚨 Starting alert manager...
🧹 Starting metric cleanup...
🌐 Metrics API: http://localhost:9090
```

**Test the monitoring service**:
```bash
# Prometheus-format metrics
curl http://localhost:9090/metrics

# JSON metrics API
curl http://localhost:9090/api/metrics

# View alerts
curl http://localhost:9090/alerts

# Service status
curl http://localhost:9090/status

# Health check
curl http://localhost:9090/health

# Get specific metric
curl http://localhost:9090/metrics/cpu_usage_percent
```

**Watch the logs** to see:
- Metrics being collected every 10-30 seconds
- Alert evaluations every 30 seconds
- Possible alerts being triggered/resolved

---

## Running Multiple Examples Simultaneously

You can run multiple examples at the same time since they use different ports:

```bash
# Terminal 1: Web Server (port 8080)
cd examples/web-server && go run main.go

# Terminal 2: Worker Pool (port 8080 - different from web server)
cd examples/worker-pool && go run main.go

# Terminal 3: Monitoring (port 9090)
cd examples/monitoring && go run main.go

# Terminal 4: Microservice (port 8080 - will conflict, so run separately)
cd examples/microservice && go run main.go
```

**Note**: The web server, worker pool, and microservice all use port 8080, so you can only run one at a time unless you modify the port in the code.

## Testing All HTTP Endpoints

Here's a comprehensive test script you can save and run:

```bash
#!/bin/bash
# save as test_examples.sh and run: chmod +x test_examples.sh && ./test_examples.sh

echo "Testing Web Server (port 8080)..."
curl -s http://localhost:8080/health | jq '.' 2>/dev/null || curl -s http://localhost:8080/health
curl -s http://localhost:8080/api/status | jq '.' 2>/dev/null || curl -s http://localhost:8080/api/status

echo -e "\nTesting Worker Pool (port 8080)..."
curl -s http://localhost:8080/stats | jq '.' 2>/dev/null || curl -s http://localhost:8080/stats
curl -s http://localhost:8080/status | jq '.' 2>/dev/null || curl -s http://localhost:8080/status

echo -e "\nTesting Monitoring Service (port 9090)..."
curl -s http://localhost:9090/health | jq '.' 2>/dev/null || curl -s http://localhost:9090/health
curl -s http://localhost:9090/alerts | jq '.' 2>/dev/null || curl -s http://localhost:9090/alerts

echo -e "\nTesting Microservice (port 8080)..."
curl -s http://localhost:8080/health | jq '.' 2>/dev/null || curl -s http://localhost:8080/health
curl -s http://localhost:8080/users | jq '.' 2>/dev/null || curl -s http://localhost:8080/users
```

## Troubleshooting

### Port Already in Use
If you see "address already in use" errors:
```bash
# Find what's using port 8080
lsof -i :8080

# Kill the process if needed
kill -9 <PID>
```

### Module Dependencies
If you see module-related errors:
```bash
# From the main lifecycle directory
go mod tidy

# From each example directory
cd examples/web-server && go mod tidy
cd examples/microservice && go mod tidy
# etc.
```

### SQLite Issues (Microservice Example)
If you see CGO or SQLite errors:
```bash
# Install CGO dependencies on macOS
xcode-select --install

# On Linux
sudo apt-get install build-essential

# Alternative: Use pure Go database
# Edit microservice/main.go and change to use different database
```

## What to Look For

When running examples, watch for:

1. **Startup sequence**: Services start in the correct order
2. **Background activity**: Periodic tasks running automatically  
3. **HTTP responses**: APIs returning proper JSON/data
4. **Graceful shutdown**: Press Ctrl+C to see coordinated shutdown
5. **Error handling**: Try invalid requests to see error responses
6. **Resource cleanup**: Notice temporary files, connections being cleaned up

Each example demonstrates different aspects of production lifecycle management!