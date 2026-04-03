package polaris

import (
	"errors"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var metricsRegistrationMu sync.Mutex

// Metrics defines Polaris-related monitoring metrics
type Metrics struct {
	// SDK operation metrics
	sdkOperationsTotal    *prometheus.CounterVec
	sdkOperationsDuration *prometheus.HistogramVec
	sdkErrorsTotal        *prometheus.CounterVec

	// Service discovery metrics
	serviceDiscoveryTotal    *prometheus.CounterVec
	serviceDiscoveryDuration *prometheus.HistogramVec
	serviceInstancesTotal    *prometheus.GaugeVec

	// Service registration metrics
	serviceRegistrationTotal    *prometheus.CounterVec
	serviceRegistrationDuration *prometheus.HistogramVec
	serviceHeartbeatTotal       *prometheus.CounterVec

	// Configuration management metrics
	configOperationsTotal    *prometheus.CounterVec
	configOperationsDuration *prometheus.HistogramVec
	configChangesTotal       *prometheus.CounterVec

	// Routing metrics
	routeOperationsTotal    *prometheus.CounterVec
	routeOperationsDuration *prometheus.HistogramVec

	// Rate limiting metrics
	rateLimitRequestsTotal *prometheus.CounterVec
	rateLimitRejectedTotal *prometheus.CounterVec
	rateLimitQuotaUsed     *prometheus.GaugeVec

	// Health check metrics
	healthCheckTotal    *prometheus.CounterVec
	healthCheckDuration *prometheus.HistogramVec
	healthCheckFailed   *prometheus.CounterVec

	// Connection metrics
	connectionTotal       *prometheus.GaugeVec
	connectionErrorsTotal *prometheus.CounterVec
}

// NewPolarisMetrics creates new monitoring metrics instance
func NewPolarisMetrics() *Metrics {
	metricsRegistrationMu.Lock()
	defer metricsRegistrationMu.Unlock()

	return &Metrics{
		// SDK operation metrics
		sdkOperationsTotal: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "sdk_operations_total",
				Help:      "Total number of SDK operations",
			},
			[]string{"operation", "status"},
		),
		sdkOperationsDuration: registerHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "sdk_operations_duration_seconds",
				Help:      "Duration of SDK operations",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"operation"},
		),
		sdkErrorsTotal: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "sdk_errors_total",
				Help:      "Total number of SDK errors",
			},
			[]string{"operation", "error_type"},
		),

		// Service discovery metrics
		serviceDiscoveryTotal: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "service_discovery_total",
				Help:      "Total number of service discovery operations",
			},
			[]string{"service", "namespace", "status"},
		),
		serviceDiscoveryDuration: registerHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "service_discovery_duration_seconds",
				Help:      "Duration of service discovery operations",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"service", "namespace"},
		),
		serviceInstancesTotal: registerGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "service_instances_total",
				Help:      "Total number of service instances",
			},
			[]string{"service", "namespace", "status"},
		),

		// Service registration metrics
		serviceRegistrationTotal: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "service_registration_total",
				Help:      "Total number of service registration operations",
			},
			[]string{"service", "namespace", "status"},
		),
		serviceRegistrationDuration: registerHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "service_registration_duration_seconds",
				Help:      "Duration of service registration operations",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"service", "namespace"},
		),
		serviceHeartbeatTotal: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "service_heartbeat_total",
				Help:      "Total number of service heartbeat operations",
			},
			[]string{"service", "namespace", "status"},
		),

		// Configuration management metrics
		configOperationsTotal: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "config_operations_total",
				Help:      "Total number of config operations",
			},
			[]string{"operation", "file", "group", "status"},
		),
		configOperationsDuration: registerHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "config_operations_duration_seconds",
				Help:      "Duration of config operations",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"operation", "file", "group"},
		),
		configChangesTotal: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "config_changes_total",
				Help:      "Total number of config changes",
			},
			[]string{"file", "group"},
		),

		// Routing metrics
		routeOperationsTotal: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "route_operations_total",
				Help:      "Total number of route operations",
			},
			[]string{"service", "namespace", "status"},
		),
		routeOperationsDuration: registerHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "route_operations_duration_seconds",
				Help:      "Duration of route operations",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"service", "namespace"},
		),

		// Rate limiting metrics
		rateLimitRequestsTotal: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "rate_limit_requests_total",
				Help:      "Total number of rate limit requests",
			},
			[]string{"service", "namespace", "status"},
		),
		rateLimitRejectedTotal: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "rate_limit_rejected_total",
				Help:      "Total number of rate limit rejections",
			},
			[]string{"service", "namespace"},
		),
		rateLimitQuotaUsed: registerGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "rate_limit_quota_used",
				Help:      "Rate limit quota usage",
			},
			[]string{"service", "namespace"},
		),

		// Health check metrics
		healthCheckTotal: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "health_check_total",
				Help:      "Total number of health checks",
			},
			[]string{"component", "status"},
		),
		healthCheckDuration: registerHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "health_check_duration_seconds",
				Help:      "Duration of health checks",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"component"},
		),
		healthCheckFailed: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "health_check_failed_total",
				Help:      "Total number of failed health checks",
			},
			[]string{"component", "error_type"},
		),

		// Connection metrics
		connectionTotal: registerGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "connection_total",
				Help:      "Total number of connections",
			},
			[]string{"type", "status"},
		),
		connectionErrorsTotal: registerCounterVec(
			prometheus.CounterOpts{
				Namespace: "lynx",
				Subsystem: "polaris",
				Name:      "connection_errors_total",
				Help:      "Total number of connection errors",
			},
			[]string{"type", "error_type"},
		),
	}
}

func registerCounterVec(opts prometheus.CounterOpts, labelNames []string) *prometheus.CounterVec {
	collector := prometheus.NewCounterVec(opts, labelNames)
	if err := prometheus.DefaultRegisterer.Register(collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(*prometheus.CounterVec)
			if !ok {
				panic(fmt.Sprintf("unexpected counter collector type for %s_%s_%s", opts.Namespace, opts.Subsystem, opts.Name))
			}
			return existing
		}
		panic(fmt.Sprintf("failed to register counter collector %s_%s_%s: %v", opts.Namespace, opts.Subsystem, opts.Name, err))
	}
	return collector
}

func registerHistogramVec(opts prometheus.HistogramOpts, labelNames []string) *prometheus.HistogramVec {
	collector := prometheus.NewHistogramVec(opts, labelNames)
	if err := prometheus.DefaultRegisterer.Register(collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(*prometheus.HistogramVec)
			if !ok {
				panic(fmt.Sprintf("unexpected histogram collector type for %s_%s_%s", opts.Namespace, opts.Subsystem, opts.Name))
			}
			return existing
		}
		panic(fmt.Sprintf("failed to register histogram collector %s_%s_%s: %v", opts.Namespace, opts.Subsystem, opts.Name, err))
	}
	return collector
}

func registerGaugeVec(opts prometheus.GaugeOpts, labelNames []string) *prometheus.GaugeVec {
	collector := prometheus.NewGaugeVec(opts, labelNames)
	if err := prometheus.DefaultRegisterer.Register(collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			existing, ok := alreadyRegistered.ExistingCollector.(*prometheus.GaugeVec)
			if !ok {
				panic(fmt.Sprintf("unexpected gauge collector type for %s_%s_%s", opts.Namespace, opts.Subsystem, opts.Name))
			}
			return existing
		}
		panic(fmt.Sprintf("failed to register gauge collector %s_%s_%s: %v", opts.Namespace, opts.Subsystem, opts.Name, err))
	}
	return collector
}

// collectors returns all Prometheus collectors for unregister on plugin unload
func (m *Metrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.sdkOperationsTotal, m.sdkOperationsDuration, m.sdkErrorsTotal,
		m.serviceDiscoveryTotal, m.serviceDiscoveryDuration, m.serviceInstancesTotal,
		m.serviceRegistrationTotal, m.serviceRegistrationDuration, m.serviceHeartbeatTotal,
		m.configOperationsTotal, m.configOperationsDuration, m.configChangesTotal,
		m.routeOperationsTotal, m.routeOperationsDuration,
		m.rateLimitRequestsTotal, m.rateLimitRejectedTotal, m.rateLimitQuotaUsed,
		m.healthCheckTotal, m.healthCheckDuration, m.healthCheckFailed,
		m.connectionTotal, m.connectionErrorsTotal,
	}
}

// Unregister unregisters all metrics from the default Prometheus registry (call on plugin cleanup)
func (m *Metrics) Unregister() {
	for _, c := range m.collectors() {
		_ = prometheus.DefaultRegisterer.Unregister(c)
	}
}

// RecordSDKOperation records SDK operation
func (m *Metrics) RecordSDKOperation(operation, status string) {
	m.sdkOperationsTotal.WithLabelValues(operation, status).Inc()
}

// RecordSDKOperationDuration records SDK operation duration
func (m *Metrics) RecordSDKOperationDuration(operation string, duration float64) {
	m.sdkOperationsDuration.WithLabelValues(operation).Observe(duration)
}

// RecordSDKError records SDK error
func (m *Metrics) RecordSDKError(operation, errorType string) {
	m.sdkErrorsTotal.WithLabelValues(operation, errorType).Inc()
}

// RecordServiceDiscovery records service discovery operation
func (m *Metrics) RecordServiceDiscovery(service, namespace, status string) {
	m.serviceDiscoveryTotal.WithLabelValues(service, namespace, status).Inc()
}

// RecordServiceDiscoveryDuration records service discovery duration
func (m *Metrics) RecordServiceDiscoveryDuration(service, namespace string, duration float64) {
	m.serviceDiscoveryDuration.WithLabelValues(service, namespace).Observe(duration)
}

// SetServiceInstances sets service instance count
func (m *Metrics) SetServiceInstances(service, namespace, status string, count float64) {
	m.serviceInstancesTotal.WithLabelValues(service, namespace, status).Set(count)
}

// RecordServiceRegistration records service registration operation
func (m *Metrics) RecordServiceRegistration(service, namespace, status string) {
	m.serviceRegistrationTotal.WithLabelValues(service, namespace, status).Inc()
}

// RecordServiceRegistrationDuration records service registration duration
func (m *Metrics) RecordServiceRegistrationDuration(service, namespace string, duration float64) {
	m.serviceRegistrationDuration.WithLabelValues(service, namespace).Observe(duration)
}

// RecordServiceHeartbeat records service heartbeat
func (m *Metrics) RecordServiceHeartbeat(service, namespace, status string) {
	m.serviceHeartbeatTotal.WithLabelValues(service, namespace, status).Inc()
}

// RecordConfigOperation records configuration operation
func (m *Metrics) RecordConfigOperation(operation, file, group, status string) {
	m.configOperationsTotal.WithLabelValues(operation, file, group, status).Inc()
}

// RecordConfigOperationDuration records configuration operation duration
func (m *Metrics) RecordConfigOperationDuration(operation, file, group string, duration float64) {
	m.configOperationsDuration.WithLabelValues(operation, file, group).Observe(duration)
}

// RecordConfigChange records configuration change
func (m *Metrics) RecordConfigChange(file, group string) {
	m.configChangesTotal.WithLabelValues(file, group).Inc()
}

// RecordRouteOperation records route operation
func (m *Metrics) RecordRouteOperation(service, namespace, status string) {
	m.routeOperationsTotal.WithLabelValues(service, namespace, status).Inc()
}

// RecordRouteOperationDuration records route operation duration
func (m *Metrics) RecordRouteOperationDuration(service, namespace string, duration float64) {
	m.routeOperationsDuration.WithLabelValues(service, namespace).Observe(duration)
}

// RecordRateLimitRequest records rate limit request
func (m *Metrics) RecordRateLimitRequest(service, namespace, status string) {
	m.rateLimitRequestsTotal.WithLabelValues(service, namespace, status).Inc()
}

// RecordRateLimitRejection records rate limit rejection
func (m *Metrics) RecordRateLimitRejection(service, namespace string) {
	m.rateLimitRejectedTotal.WithLabelValues(service, namespace).Inc()
}

// SetRateLimitQuota sets rate limit quota usage
func (m *Metrics) SetRateLimitQuota(service, namespace string, quota float64) {
	m.rateLimitQuotaUsed.WithLabelValues(service, namespace).Set(quota)
}

// RecordHealthCheck records health check
func (m *Metrics) RecordHealthCheck(component, status string) {
	m.healthCheckTotal.WithLabelValues(component, status).Inc()
}

// RecordHealthCheckDuration records health check duration
func (m *Metrics) RecordHealthCheckDuration(component string, duration float64) {
	m.healthCheckDuration.WithLabelValues(component).Observe(duration)
}

// RecordHealthCheckFailed records health check failure
func (m *Metrics) RecordHealthCheckFailed(component, errorType string) {
	m.healthCheckFailed.WithLabelValues(component, errorType).Inc()
}

// SetConnectionCount sets connection count
func (m *Metrics) SetConnectionCount(connType, status string, count float64) {
	m.connectionTotal.WithLabelValues(connType, status).Set(count)
}

// RecordConnectionError records connection error
func (m *Metrics) RecordConnectionError(connType, errorType string) {
	m.connectionErrorsTotal.WithLabelValues(connType, errorType).Inc()
}
