# Lifecycle Examples

This directory contains real-world examples demonstrating how to use the lifecycle library in production scenarios. Each example showcases different aspects of application lifecycle management with comprehensive error handling, graceful shutdown, and resource cleanup.

## Examples Overview

### 1. **Web Server** (`web-server/`)
**Real-world scenario**: Production HTTP API server with middleware, statistics, and monitoring.

**Features demonstrated**:
- HTTP server lifecycle management
- Request middleware and logging
- Runtime statistics collection
- Graceful shutdown with connection draining
- Environment validation
- Performance metrics

**Use case**: REST APIs, web applications, HTTP microservices

---

### 2. **Microservice** (`microservice/`)
**Real-world scenario**: Complete microservice with database, cache, background workers, and health checks.

**Features demonstrated**:
- Database connection lifecycle (SQLite with connection pooling)
- In-memory caching with TTL cleanup
- Multiple background worker coordination
- HTTP API with caching layer
- Health check endpoints
- Graceful shutdown of all components
- Resource cleanup with error handling

**Use case**: User management service, API backend, data processing service

---

### 3. **CLI Tool** (`cli-tool/`)
**Real-world scenario**: Batch file processing tool with progress tracking and resumable operations.

**Features demonstrated**:
- File processing with progress tracking
- Resumable operations (saves progress on interruption)
- Temporary file management
- Signal handling for safe interruption
- Progress reporting and statistics
- Cleanup of temporary files

**Use case**: Data migration tools, batch processors, file converters, ETL tools

---

### 4. **Worker Pool** (`worker-pool/`)
**Real-world scenario**: Distributed job processing system with HTTP management API.

**Features demonstrated**:
- Worker pool management with configurable size
- Job queuing and processing
- Graceful worker shutdown with job completion
- HTTP API for job submission and monitoring
- Real-time statistics and monitoring
- Queue backpressure handling
- Worker health monitoring

**Use case**: Background job processing, image processing, data analytics, email processing

---

### 5. **Monitoring Service** (`monitoring/`)
**Real-world scenario**: Metrics collection and alerting system with Prometheus compatibility.

**Features demonstrated**:
- Multi-collector metric gathering
- Prometheus-compatible metrics endpoint
- Alert rule evaluation and management
- Metric retention and cleanup
- JSON and Prometheus format APIs
- System, application, and business metrics
- Alert lifecycle management

**Use case**: Application monitoring, infrastructure monitoring, alerting systems, observability platforms

## Running Examples

Each example is self-contained and can be run independently:

```bash
# Run the web server example
cd examples/web-server
go run main.go

# Run the microservice example
cd examples/microservice
go run main.go

# Run the CLI tool example (requires input directory)
cd examples/cli-tool
mkdir input output
echo "hello world" > input/test.txt
go run main.go ./input ./output

# Run the worker pool example
cd examples/worker-pool
go run main.go

# Run the monitoring service
cd examples/monitoring
go run main.go
```

## Testing Examples

You can test the HTTP-based examples using curl:

```bash
# Web server endpoints
curl http://localhost:8080/health
curl http://localhost:8080/api/status
curl http://localhost:8080/api/users

# Microservice endpoints
curl http://localhost:8080/users
curl http://localhost:8080/cache/stats

# Worker pool endpoints
curl http://localhost:8080/stats
curl -X POST http://localhost:8080/submit -d '{"type":"test","payload":{"msg":"hello"},"duration_ms":1000}'

# Monitoring service endpoints
curl http://localhost:9090/metrics
curl http://localhost:9090/api/metrics
curl http://localhost:9090/alerts
```

## Common Patterns Demonstrated

### Resource Management
- **Database connections**: Proper initialization, health checks, graceful closure
- **File handles**: Temporary file cleanup, progress file management
- **Network connections**: Connection pooling, timeout handling
- **Memory management**: Cache cleanup, metric retention policies

### Graceful Shutdown
- **HTTP servers**: Connection draining, request completion
- **Background workers**: Job completion before shutdown
- **Database operations**: Transaction completion, connection cleanup
- **File operations**: Safe file closure, temporary file cleanup

### Error Handling
- **Connection failures**: Database unavailable, network timeouts
- **Processing errors**: Job failures, file processing errors
- **Resource exhaustion**: Queue full, memory limits
- **Timeout handling**: Operation timeouts, shutdown timeouts

### Signal Handling
- **SIGINT/SIGTERM**: Graceful shutdown initiation
- **Signal propagation**: Coordinated shutdown across components
- **Progress preservation**: Save state for resumable operations
- **Statistics reporting**: Final metrics and status reporting

### Background Tasks
- **Periodic jobs**: Metric collection, cache cleanup, health checks
- **Queue processing**: Job processing, result handling
- **File watching**: Directory monitoring, file processing
- **Maintenance tasks**: Log rotation, metric aggregation

### Multi-Service Coordination
- **Startup order**: Database → Cache → Workers → HTTP Server
- **Shutdown order**: HTTP Server → Workers → Cache → Database
- **Dependency management**: Service health checks, readiness probes
- **Resource sharing**: Connection pools, shared caches

## Production Considerations

These examples demonstrate production-ready patterns:

1. **Configuration Management**: Environment validation, default values
2. **Observability**: Metrics, logging, health checks, status endpoints
3. **Error Recovery**: Retry logic, circuit breakers, fallback mechanisms
4. **Performance**: Connection pooling, caching, async processing
5. **Security**: Input validation, timeout enforcement, resource limits
6. **Scalability**: Worker pools, queue management, load balancing preparation

Each example can serve as a starting point for real production services, with the lifecycle library handling the complex orchestration of startup, shutdown, and error scenarios.