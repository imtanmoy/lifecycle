package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/imtanmoy/lifecycle"
)

// MetricPoint represents a single metric measurement
type MetricPoint struct {
	Name      string                 `json:"name"`
	Value     float64                `json:"value"`
	Labels    map[string]string      `json:"labels"`
	Timestamp time.Time              `json:"timestamp"`
	Type      string                 `json:"type"` // counter, gauge, histogram
}

// Alert represents a monitoring alert
type Alert struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Severity    string            `json:"severity"` // info, warning, critical
	Labels      map[string]string `json:"labels"`
	Triggered   time.Time         `json:"triggered"`
	Resolved    *time.Time        `json:"resolved,omitempty"`
	Active      bool              `json:"active"`
}

// MonitoringService collects metrics and manages alerts
type MonitoringService struct {
	// Metrics storage
	mu           sync.RWMutex
	metrics      map[string][]MetricPoint
	metricCounts map[string]int64
	
	// Alerting
	alerts       map[string]*Alert
	alertRules   []AlertRule
	alertCount   int64
	
	// Service state
	startTime    time.Time
	collectors   map[string]*MetricCollector
	isCollecting int32
}

// AlertRule defines conditions for triggering alerts
type AlertRule struct {
	Name        string
	MetricName  string
	Condition   string  // "greater_than", "less_than", "equals"
	Threshold   float64
	Duration    time.Duration
	Severity    string
	Description string
}

// MetricCollector collects specific types of metrics
type MetricCollector struct {
	Name        string
	Interval    time.Duration
	CollectFunc func() MetricPoint
	stopChan    chan struct{}
}

func main() {
	service := &MonitoringService{
		metrics:      make(map[string][]MetricPoint),
		metricCounts: make(map[string]int64),
		alerts:       make(map[string]*Alert),
		collectors:   make(map[string]*MetricCollector),
		startTime:    time.Now(),
	}

	// Setup HTTP server for monitoring API
	mux := http.NewServeMux()
	
	// Metrics endpoints
	mux.HandleFunc("/metrics", service.metricsHandler)           // Prometheus-style metrics
	mux.HandleFunc("/metrics/", service.specificMetricHandler)  // Get specific metric
	mux.HandleFunc("/api/metrics", service.apiMetricsHandler)   // JSON metrics API
	
	// Alerting endpoints
	mux.HandleFunc("/alerts", service.alertsHandler)
	mux.HandleFunc("/alerts/", service.specificAlertHandler)
	
	// Health and status
	mux.HandleFunc("/health", service.healthHandler)
	mux.HandleFunc("/status", service.statusHandler)
	
	server := &http.Server{
		Addr:    ":9090",
		Handler: mux,
	}

	// Create monitoring service lifecycle
	lc := lifecycle.New(func(hooks *lifecycle.Hooks, opts *lifecycle.Options) {
		opts.ShutdownTimeout = 30 * time.Second

		// === PRE-START: Initialize alert rules ===
		hooks.OnPreStart = append(hooks.OnPreStart, func(ctx context.Context) error {
			log.Println("🔍 Initializing monitoring service...")
			
			// Setup alert rules
			service.alertRules = []AlertRule{
				{
					Name:        "high_cpu_usage",
					MetricName:  "cpu_usage_percent",
					Condition:   "greater_than",
					Threshold:   80.0,
					Duration:    2 * time.Minute,
					Severity:    "warning",
					Description: "CPU usage is above 80% for more than 2 minutes",
				},
				{
					Name:        "memory_critical",
					MetricName:  "memory_usage_percent",
					Condition:   "greater_than",
					Threshold:   95.0,
					Duration:    30 * time.Second,
					Severity:    "critical",
					Description: "Memory usage is critically high (>95%)",
				},
				{
					Name:        "disk_space_low",
					MetricName:  "disk_usage_percent",
					Condition:   "greater_than",
					Threshold:   90.0,
					Duration:    5 * time.Minute,
					Severity:    "warning",
					Description: "Disk usage is above 90%",
				},
			}
			
			log.Printf("✅ Configured %d alert rules", len(service.alertRules))
			return nil
		})

		// === START: Initialize metric collectors ===
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("📊 Starting metric collectors...")
			
			// System metrics collector
			service.collectors["system"] = &MetricCollector{
				Name:     "system",
				Interval: 10 * time.Second,
				CollectFunc: func() MetricPoint {
					return service.collectSystemMetrics()
				},
				stopChan: make(chan struct{}),
			}
			
			// Application metrics collector
			service.collectors["app"] = &MetricCollector{
				Name:     "application",
				Interval: 15 * time.Second,
				CollectFunc: func() MetricPoint {
					return service.collectAppMetrics()
				},
				stopChan: make(chan struct{}),
			}
			
			// Business metrics collector
			service.collectors["business"] = &MetricCollector{
				Name:     "business",
				Interval: 30 * time.Second,
				CollectFunc: func() MetricPoint {
					return service.collectBusinessMetrics()
				},
				stopChan: make(chan struct{}),
			}
			
			log.Printf("✅ Configured %d metric collectors", len(service.collectors))
			return nil
		})

		// Start collectors
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("🔄 Starting metric collection...")
			
			atomic.StoreInt32(&service.isCollecting, 1)
			
			for name, collector := range service.collectors {
				go service.runCollector(ctx, collector)
				log.Printf("✅ Started %s collector (interval: %v)", name, collector.Interval)
			}
			
			return nil
		})

		// Start alert manager
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("🚨 Starting alert manager...")
			go service.runAlertManager(ctx)
			log.Println("✅ Alert manager started")
			return nil
		})

		// Start metric cleanup
		hooks.OnStart = append(hooks.OnStart, func(ctx context.Context) error {
			log.Println("🧹 Starting metric cleanup...")
			go service.runMetricCleanup(ctx)
			log.Println("✅ Metric cleanup started")
			return nil
		})

		// === SIGNAL HANDLING ===
		hooks.OnSignal = append(hooks.OnSignal, func(ctx context.Context) error {
			log.Println("📡 Shutdown signal received...")
			
			// Stop collecting new metrics
			atomic.StoreInt32(&service.isCollecting, 0)
			
			// Log final statistics
			service.mu.RLock()
			totalMetrics := int64(0)
			for _, count := range service.metricCounts {
				totalMetrics += count
			}
			totalAlerts := len(service.alerts)
			service.mu.RUnlock()
			
			log.Printf("📊 Final metrics: %d total data points across %d metric types", 
				totalMetrics, len(service.metricCounts))
			log.Printf("🚨 Total alerts: %d", totalAlerts)
			
			return nil
		})

		// === SHUTDOWN: Stop collectors gracefully ===
		hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
			log.Println("🛑 Stopping metric collectors...")
			
			// Stop all collectors
			for name, collector := range service.collectors {
				close(collector.stopChan)
				log.Printf("🛑 Stopped %s collector", name)
			}
			
			log.Println("✅ All collectors stopped")
			return nil
		})

		// Export final metrics
		hooks.OnShutdown = append(hooks.OnShutdown, func(ctx context.Context) error {
			log.Println("💾 Exporting final metrics...")
			
			// In a real system, you'd export to persistent storage
			filename := fmt.Sprintf("metrics_export_%d.json", time.Now().Unix())
			service.exportMetrics(filename)
			
			log.Printf("✅ Metrics exported to %s", filename)
			return nil
		})

		// === EXIT: Final cleanup ===
		hooks.OnExit = append(hooks.OnExit, func(ctx context.Context) error {
			uptime := time.Since(service.startTime)
			
			service.mu.RLock()
			totalMetrics := int64(0)
			for _, count := range service.metricCounts {
				totalMetrics += count
			}
			totalAlerts := len(service.alerts)
			activeAlerts := 0
			for _, alert := range service.alerts {
				if alert.Active {
					activeAlerts++
				}
			}
			service.mu.RUnlock()
			
			log.Println("📈 Final Monitoring Statistics:")
			log.Printf("   Uptime: %v", uptime)
			log.Printf("   Total Metrics Collected: %d", totalMetrics)
			log.Printf("   Metric Types: %d", len(service.metricCounts))
			log.Printf("   Total Alerts: %d", totalAlerts)
			log.Printf("   Active Alerts: %d", activeAlerts)
			
			if totalMetrics > 0 {
				rate := float64(totalMetrics) / uptime.Seconds()
				log.Printf("   Collection Rate: %.2f metrics/sec", rate)
			}
			
			log.Println("✨ Monitoring service shutdown complete!")
			return nil
		})
		
	}).AttachHTTPServer(server)

	// Start the monitoring service
	log.Println("🌟 Monitoring Service Starting...")
	log.Printf("🌐 Metrics API: http://localhost%s", server.Addr)
	log.Println("📋 Available endpoints:")
	log.Println("   GET /metrics - Prometheus metrics")
	log.Println("   GET /api/metrics - JSON metrics API")
	log.Println("   GET /alerts - Active alerts")
	log.Println("   GET /health - Service health")
	log.Println("   GET /status - Service status")
	log.Println("💡 Press Ctrl+C to gracefully shutdown")

	if err := lc.Run(context.Background()); err != nil {
		log.Fatalf("❌ Monitoring service failed: %v", err)
	}
}

// Metric collection methods
func (ms *MonitoringService) collectSystemMetrics() MetricPoint {
	// Simulate system metrics collection
	metrics := []MetricPoint{
		{
			Name:      "cpu_usage_percent",
			Value:     30 + rand.Float64()*50, // 30-80%
			Labels:    map[string]string{"host": "localhost", "core": "total"},
			Timestamp: time.Now(),
			Type:      "gauge",
		},
		{
			Name:      "memory_usage_percent",
			Value:     40 + rand.Float64()*40, // 40-80%
			Labels:    map[string]string{"host": "localhost", "type": "physical"},
			Timestamp: time.Now(),
			Type:      "gauge",
		},
		{
			Name:      "disk_usage_percent",
			Value:     60 + rand.Float64()*30, // 60-90%
			Labels:    map[string]string{"host": "localhost", "mount": "/"},
			Timestamp: time.Now(),
			Type:      "gauge",
		},
	}
	
	// Return random metric
	return metrics[rand.Intn(len(metrics))]
}

func (ms *MonitoringService) collectAppMetrics() MetricPoint {
	// Simulate application metrics
	metrics := []MetricPoint{
		{
			Name:      "http_requests_total",
			Value:     float64(rand.Intn(100)),
			Labels:    map[string]string{"method": "GET", "status": "200"},
			Timestamp: time.Now(),
			Type:      "counter",
		},
		{
			Name:      "response_time_seconds",
			Value:     0.001 + rand.Float64()*0.5, // 1ms to 500ms
			Labels:    map[string]string{"endpoint": "/api/metrics"},
			Timestamp: time.Now(),
			Type:      "histogram",
		},
	}
	
	return metrics[rand.Intn(len(metrics))]
}

func (ms *MonitoringService) collectBusinessMetrics() MetricPoint {
	// Simulate business metrics
	return MetricPoint{
		Name:      "active_users",
		Value:     float64(1000 + rand.Intn(500)),
		Labels:    map[string]string{"region": "us-east-1"},
		Timestamp: time.Now(),
		Type:      "gauge",
	}
}

// Collector runner
func (ms *MonitoringService) runCollector(ctx context.Context, collector *MetricCollector) {
	ticker := time.NewTicker(collector.Interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			if atomic.LoadInt32(&ms.isCollecting) == 0 {
				continue
			}
			
			// Collect metric
			metric := collector.CollectFunc()
			
			// Store metric
			ms.mu.Lock()
			if ms.metrics[metric.Name] == nil {
				ms.metrics[metric.Name] = make([]MetricPoint, 0)
			}
			ms.metrics[metric.Name] = append(ms.metrics[metric.Name], metric)
			ms.metricCounts[metric.Name]++
			ms.mu.Unlock()
			
		case <-collector.stopChan:
			log.Printf("📊 Collector %s stopped", collector.Name)
			return
		case <-ctx.Done():
			return
		}
	}
}

// Alert manager
func (ms *MonitoringService) runAlertManager(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			ms.evaluateAlerts()
		case <-ctx.Done():
			log.Println("🚨 Alert manager stopped")
			return
		}
	}
}

func (ms *MonitoringService) evaluateAlerts() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	for _, rule := range ms.alertRules {
		metrics, exists := ms.metrics[rule.MetricName]
		if !exists || len(metrics) == 0 {
			continue
		}
		
		// Get latest metric value
		latest := metrics[len(metrics)-1]
		shouldAlert := false
		
		switch rule.Condition {
		case "greater_than":
			shouldAlert = latest.Value > rule.Threshold
		case "less_than":
			shouldAlert = latest.Value < rule.Threshold
		case "equals":
			shouldAlert = latest.Value == rule.Threshold
		}
		
		alertID := fmt.Sprintf("%s_%s", rule.Name, rule.MetricName)
		
		if shouldAlert {
			if _, exists := ms.alerts[alertID]; !exists {
				// Create new alert
				alert := &Alert{
					ID:          alertID,
					Name:        rule.Name,
					Description: rule.Description,
					Severity:    rule.Severity,
					Labels: map[string]string{
						"metric": rule.MetricName,
						"value":  fmt.Sprintf("%.2f", latest.Value),
						"threshold": fmt.Sprintf("%.2f", rule.Threshold),
					},
					Triggered: time.Now(),
					Active:    true,
				}
				
				ms.alerts[alertID] = alert
				ms.alertCount++
				
				log.Printf("🚨 Alert triggered: %s - %s (value: %.2f, threshold: %.2f)", 
					rule.Severity, rule.Name, latest.Value, rule.Threshold)
			}
		} else {
			// Resolve alert if it exists
			if alert, exists := ms.alerts[alertID]; exists && alert.Active {
				now := time.Now()
				alert.Resolved = &now
				alert.Active = false
				
				log.Printf("✅ Alert resolved: %s - %s", rule.Severity, rule.Name)
			}
		}
	}
}

// Metric cleanup
func (ms *MonitoringService) runMetricCleanup(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			ms.cleanupOldMetrics()
		case <-ctx.Done():
			log.Println("🧹 Metric cleanup stopped")
			return
		}
	}
}

func (ms *MonitoringService) cleanupOldMetrics() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	cutoff := time.Now().Add(-1 * time.Hour) // Keep last hour
	cleaned := 0
	
	for name, points := range ms.metrics {
		newPoints := make([]MetricPoint, 0)
		for _, point := range points {
			if point.Timestamp.After(cutoff) {
				newPoints = append(newPoints, point)
			} else {
				cleaned++
			}
		}
		ms.metrics[name] = newPoints
	}
	
	if cleaned > 0 {
		log.Printf("🧹 Cleaned %d old metric points", cleaned)
	}
}

// Export metrics
func (ms *MonitoringService) exportMetrics(filename string) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	export := map[string]interface{}{
		"timestamp":     time.Now(),
		"export_type":   "final_metrics",
		"metrics":       ms.metrics,
		"metric_counts": ms.metricCounts,
		"alerts":        ms.alerts,
	}
	
	// In a real system, write to file
	_ = export
	_ = filename
}

// HTTP handlers
func (ms *MonitoringService) metricsHandler(w http.ResponseWriter, r *http.Request) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	w.Header().Set("Content-Type", "text/plain")
	
	// Prometheus format
	for name, points := range ms.metrics {
		if len(points) == 0 {
			continue
		}
		
		latest := points[len(points)-1]
		labels := make([]string, 0)
		for k, v := range latest.Labels {
			labels = append(labels, fmt.Sprintf(`%s="%s"`, k, v))
		}
		
		labelStr := ""
		if len(labels) > 0 {
			labelStr = "{" + strings.Join(labels, ",") + "}"
		}
		
		fmt.Fprintf(w, "# TYPE %s %s\n", name, latest.Type)
		fmt.Fprintf(w, "%s%s %.2f %d\n", name, labelStr, latest.Value, latest.Timestamp.Unix())
	}
}

func (ms *MonitoringService) apiMetricsHandler(w http.ResponseWriter, r *http.Request) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"metrics": ms.metrics,
		"counts":  ms.metricCounts,
	})
}

func (ms *MonitoringService) alertsHandler(w http.ResponseWriter, r *http.Request) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"alerts": ms.alerts,
		"total":  len(ms.alerts),
	})
}

func (ms *MonitoringService) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"uptime":    time.Since(ms.startTime).Seconds(),
		"collecting": atomic.LoadInt32(&ms.isCollecting) == 1,
	})
}

func (ms *MonitoringService) statusHandler(w http.ResponseWriter, r *http.Request) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	activeAlerts := 0
	for _, alert := range ms.alerts {
		if alert.Active {
			activeAlerts++
		}
	}
	
	status := map[string]interface{}{
		"service":       "monitoring",
		"uptime":        time.Since(ms.startTime).Seconds(),
		"collectors":    len(ms.collectors),
		"metric_types":  len(ms.metrics),
		"active_alerts": activeAlerts,
		"total_alerts":  len(ms.alerts),
		"collecting":    atomic.LoadInt32(&ms.isCollecting) == 1,
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (ms *MonitoringService) specificMetricHandler(w http.ResponseWriter, r *http.Request) {
	// Extract metric name from URL path
	path := strings.TrimPrefix(r.URL.Path, "/metrics/")
	metricName := strings.Split(path, "/")[0]
	
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	if metrics, exists := ms.metrics[metricName]; exists {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"metric": metricName,
			"points": metrics,
			"count":  ms.metricCounts[metricName],
		})
	} else {
		http.NotFound(w, r)
	}
}

func (ms *MonitoringService) specificAlertHandler(w http.ResponseWriter, r *http.Request) {
	// Extract alert ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/alerts/")
	alertID := strings.Split(path, "/")[0]
	
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	
	if alert, exists := ms.alerts[alertID]; exists {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(alert)
	} else {
		http.NotFound(w, r)
	}
}