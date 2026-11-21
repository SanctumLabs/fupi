package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sanctumlabs/curtz/app/pkg/errdefs"
)

// OperationMetrics tracks metrics for database operations
type OperationMetrics struct {
	Name           string
	ExecutionCount int64
	SuccessCount   int64
	FailureCount   int64
	TimeoutCount   int64
	TotalDuration  time.Duration
	AvgDuration    time.Duration
	MaxDuration    time.Duration
	MinDuration    time.Duration
	LastExecuted   time.Time
	LastError      error
	LastErrorTime  time.Time
}

// DatabaseMetrics holds comprehensive database metrics
type DatabaseMetrics struct {
	mutex      sync.RWMutex
	operations map[string]*OperationMetrics

	// Connection pool metrics
	poolMetrics *PoolMetrics

	// Overall health
	isHealthy           bool
	lastHealthCheck     time.Time
	consecutiveFailures int
}

// PoolMetrics tracks connection pool statistics
type PoolMetrics struct {
	MaxConnections      int32
	ActiveConnections   int32
	IdleConnections     int32
	AcquiredConnections int32
	ConstructingConns   int32
	CancelledAcquires   int64
	AcquireCount        int64
	AcquireDuration     time.Duration
	LastPoolCheck       time.Time
}

// NewDatabaseMetrics creates a new metrics instance
func NewDatabaseMetrics() *DatabaseMetrics {
	return &DatabaseMetrics{
		operations:  make(map[string]*OperationMetrics),
		poolMetrics: &PoolMetrics{},
		isHealthy:   true,
	}
}

// RecordOperation records metrics for a database operation
func (dm *DatabaseMetrics) RecordOperation(name string, duration time.Duration, err error) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	metrics, exists := dm.operations[name]
	if !exists {
		metrics = &OperationMetrics{
			Name:        name,
			MinDuration: duration,
		}
		dm.operations[name] = metrics
	}

	// Update counters
	metrics.ExecutionCount++
	metrics.LastExecuted = time.Now()

	if err != nil {
		metrics.FailureCount++
		metrics.LastError = err
		metrics.LastErrorTime = time.Now()

		// Check for timeout errors
		if errdefs.IsTimeoutError(err) {
			metrics.TimeoutCount++
		}
	} else {
		metrics.SuccessCount++
	}

	// Update duration metrics
	metrics.TotalDuration += duration
	metrics.AvgDuration = time.Duration(int64(metrics.TotalDuration) / metrics.ExecutionCount)

	if duration > metrics.MaxDuration {
		metrics.MaxDuration = duration
	}
	if duration < metrics.MinDuration || metrics.MinDuration == 0 {
		metrics.MinDuration = duration
	}
}

// UpdatePoolMetrics updates connection pool metrics
func (dm *DatabaseMetrics) UpdatePoolMetrics(pool *pgxpool.Pool) {
	if pool == nil {
		return
	}

	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	stat := pool.Stat()
	dm.poolMetrics = &PoolMetrics{
		MaxConnections:      stat.MaxConns(),
		AcquiredConnections: stat.AcquiredConns(),
		ActiveConnections:   stat.TotalConns(),
		IdleConnections:     stat.IdleConns(),
		ConstructingConns:   stat.ConstructingConns(),
		CancelledAcquires:   stat.CanceledAcquireCount(),
		AcquireCount:        stat.AcquireCount(),
		AcquireDuration:     stat.AcquireDuration(),
		LastPoolCheck:       time.Now(),
	}
}

// GetOperationMetrics returns metrics for a specific operation
func (dm *DatabaseMetrics) GetOperationMetrics(name string) *OperationMetrics {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()

	if metrics, exists := dm.operations[name]; exists {
		// Return a copy to avoid race conditions
		copyMetrics := *metrics
		return &copyMetrics
	}
	return nil
}

// GetAllMetrics returns all operation metrics
func (dm *DatabaseMetrics) GetAllMetrics() map[string]*OperationMetrics {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()

	result := make(map[string]*OperationMetrics)
	for name, metrics := range dm.operations {
		copyMetrics := *metrics
		result[name] = &copyMetrics
	}
	return result
}

// GetPoolMetrics returns current pool metrics
func (dm *DatabaseMetrics) GetPoolMetrics() *PoolMetrics {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()

	if dm.poolMetrics == nil {
		return nil
	}

	// Return a copy
	copyMetrics := *dm.poolMetrics
	return &copyMetrics
}

// IsHealthy returns the current health status
func (dm *DatabaseMetrics) IsHealthy() bool {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()
	return dm.isHealthy
}

// UpdateHealthStatus updates the health status
func (dm *DatabaseMetrics) UpdateHealthStatus(healthy bool) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	dm.lastHealthCheck = time.Now()
	if healthy {
		dm.isHealthy = true
		dm.consecutiveFailures = 0
	} else {
		dm.consecutiveFailures++
		if dm.consecutiveFailures >= 3 {
			dm.isHealthy = false
		}
	}
}

// LogMetricsSummary logs a summary of current metrics
func (dm *DatabaseMetrics) LogMetricsSummary(ctx context.Context) {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()

	slog.InfoContext(ctx, "Database metrics summary",
		"healthy", dm.isHealthy,
		"operations_count", len(dm.operations),
		"consecutive_failures", dm.consecutiveFailures)

	// Log pool metrics
	if dm.poolMetrics != nil {
		slog.InfoContext(ctx, "Connection pool metrics",
			"max_conns", dm.poolMetrics.MaxConnections,
			"active_conns", dm.poolMetrics.ActiveConnections,
			"idle_conns", dm.poolMetrics.IdleConnections,
			"constructing_conns", dm.poolMetrics.ConstructingConns,
			"acquire_count", dm.poolMetrics.AcquireCount,
			"avg_acquire_duration", dm.poolMetrics.AcquireDuration)
	}

	// Log operation metrics for operations with failures or timeouts
	for name, metrics := range dm.operations {
		if metrics.FailureCount > 0 || metrics.TimeoutCount > 0 {
			successRate := float64(metrics.SuccessCount) / float64(metrics.ExecutionCount) * 100
			slog.WarnContext(ctx, "Operation metrics with issues",
				"operation", name,
				"executions", metrics.ExecutionCount,
				"success_rate", fmt.Sprintf("%.2f%%", successRate),
				"failures", metrics.FailureCount,
				"timeouts", metrics.TimeoutCount,
				"avg_duration", metrics.AvgDuration,
				"max_duration", metrics.MaxDuration,
				"last_error", metrics.LastError)
		}
	}
}

// GetHealthReport returns a comprehensive health report
func (dm *DatabaseMetrics) GetHealthReport() map[string]interface{} {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()

	report := map[string]interface{}{
		"overall_healthy":      dm.isHealthy,
		"last_health_check":    dm.lastHealthCheck,
		"consecutive_failures": dm.consecutiveFailures,
		"total_operations":     len(dm.operations),
	}

	// Add pool health
	if dm.poolMetrics != nil {
		poolUtilization := float64(dm.poolMetrics.ActiveConnections) / float64(dm.poolMetrics.MaxConnections) * 100
		report["pool_utilization"] = fmt.Sprintf("%.2f%%", poolUtilization)
		report["pool_exhaustion_risk"] = poolUtilization > 90
	}

	// Add operation health summary
	totalOperations := int64(0)
	totalFailures := int64(0)
	totalTimeouts := int64(0)
	problematicOperations := []string{}

	for name, metrics := range dm.operations {
		totalOperations += metrics.ExecutionCount
		totalFailures += metrics.FailureCount
		totalTimeouts += metrics.TimeoutCount

		if metrics.FailureCount > 0 {
			failureRate := float64(metrics.FailureCount) / float64(metrics.ExecutionCount) * 100
			if failureRate > 10 { // More than 10% failure rate
				problematicOperations = append(problematicOperations, name)
			}
		}
	}

	if totalOperations > 0 {
		report["overall_success_rate"] = fmt.Sprintf("%.2f%%",
			float64(totalOperations-totalFailures)/float64(totalOperations)*100)
		report["timeout_rate"] = fmt.Sprintf("%.2f%%",
			float64(totalTimeouts)/float64(totalOperations)*100)
	}

	report["problematic_operations"] = problematicOperations

	return report
}

// Global metrics instance
var globalMetrics = NewDatabaseMetrics()

// GetGlobalMetrics returns the global metrics instance
func GetGlobalMetrics() *DatabaseMetrics {
	return globalMetrics
}

// MonitoredOperation wraps an operation with metrics collection
func MonitoredOperation[T any](
	ctx context.Context,
	operationName string,
	operation func(ctx context.Context) (T, error),
) (T, error) {
	start := time.Now()

	result, err := operation(ctx)

	duration := time.Since(start)
	globalMetrics.RecordOperation(operationName, duration, err)

	// Log slow operations
	if duration > 5*time.Second {
		slog.WarnContext(ctx, "Slow database operation detected",
			"operation", operationName,
			"duration", duration,
			"error", err)
	}

	return result, err
}

// StartMetricsCollector starts a background goroutine to collect metrics
func StartMetricsCollector(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	if interval == 0 {
		interval = 30 * time.Second // Default interval
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		lastLogTime := time.Now()

		for {
			select {
			case <-ctx.Done():
				slog.InfoContext(ctx, "Stopping metrics collector")
				return
			case <-ticker.C:
				// Update pool metrics
				globalMetrics.UpdatePoolMetrics(pool)

				// Perform health check
				healthy := performHealthCheck(ctx, pool)
				globalMetrics.UpdateHealthStatus(healthy)

				// Log summary every 5 minutes
				if time.Since(lastLogTime) >= 5*time.Minute { // Every 5 minutes
					globalMetrics.LogMetricsSummary(ctx)
					lastLogTime = time.Now()
				}
			}
		}
	}()

}

// performHealthCheck performs a basic health check on the database
func performHealthCheck(ctx context.Context, pool *pgxpool.Pool) bool {
	if pool == nil {
		return false
	}

	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(healthCtx); err != nil {
		slog.ErrorContext(ctx, "Database health check failed", "error", err)
		return false
	}

	// Check if pool is severely constrained
	stat := pool.Stat()
	if stat.AcquiredConns() >= stat.MaxConns()-1 {
		slog.WarnContext(ctx, "Connection pool nearly exhausted",
			"acquired", stat.AcquiredConns(),
			"max", stat.MaxConns())
		// Still healthy, just constrained
	}

	return true
}

// AlertThresholds defines thresholds for alerting
type AlertThresholds struct {
	MaxFailureRate     float64       // Maximum acceptable failure rate (percentage)
	MaxTimeoutRate     float64       // Maximum acceptable timeout rate (percentage)
	MaxAvgDuration     time.Duration // Maximum acceptable average duration
	MaxPoolUtilization float64       // Maximum acceptable pool utilization (percentage)
}

// DefaultAlertThresholds returns sensible default alert thresholds
func DefaultAlertThresholds() AlertThresholds {
	return AlertThresholds{
		MaxFailureRate:     5.0, // 5% failure rate
		MaxTimeoutRate:     2.0, // 2% timeout rate
		MaxAvgDuration:     2 * time.Second,
		MaxPoolUtilization: 85.0, // 85% pool utilization
	}
}

// CheckAlerts checks if any metrics exceed alert thresholds
func (dm *DatabaseMetrics) CheckAlerts(thresholds AlertThresholds) []string {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()

	var alerts []string

	// Check pool utilization
	if dm.poolMetrics != nil && dm.poolMetrics.MaxConnections > 0 {
		utilization := float64(dm.poolMetrics.ActiveConnections) / float64(dm.poolMetrics.MaxConnections) * 100
		if utilization > thresholds.MaxPoolUtilization {
			alerts = append(alerts, fmt.Sprintf("High pool utilization: %.2f%%", utilization))
		}
	}

	// Check operation metrics
	for name, metrics := range dm.operations {
		if metrics.ExecutionCount == 0 {
			continue
		}

		// Check failure rate
		failureRate := float64(metrics.FailureCount) / float64(metrics.ExecutionCount) * 100
		if failureRate > thresholds.MaxFailureRate {
			alerts = append(alerts, fmt.Sprintf("High failure rate for %s: %.2f%%", name, failureRate))
		}

		// Check timeout rate
		timeoutRate := float64(metrics.TimeoutCount) / float64(metrics.ExecutionCount) * 100
		if timeoutRate > thresholds.MaxTimeoutRate {
			alerts = append(alerts, fmt.Sprintf("High timeout rate for %s: %.2f%%", name, timeoutRate))
		}

		// Check average duration
		if metrics.AvgDuration > thresholds.MaxAvgDuration {
			alerts = append(alerts, fmt.Sprintf("Slow average duration for %s: %v", name, metrics.AvgDuration))
		}
	}

	return alerts
}
