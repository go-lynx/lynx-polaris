package polaris

import (
	"context"
	"testing"
	"time"

	"github.com/go-lynx/lynx-polaris/conf"
	"github.com/polarismesh/polaris-go/pkg/model"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/durationpb"
)

// TestPolarisPlugin_Initialization tests plugin initialization
func TestPolarisPlugin_Initialization(t *testing.T) {
	// Create plugin instance
	plugin := NewPolarisControlPlane()
	assert.NotNil(t, plugin)
	assert.Equal(t, pluginName, plugin.Name())
	assert.Equal(t, pluginVersion, plugin.Version())
	assert.Equal(t, pluginDescription, plugin.Description())
}

// TestPolarisPlugin_Configuration tests configuration management
func TestPolarisPlugin_Configuration(t *testing.T) {
	plugin := NewPolarisControlPlane()

	// Test plugin basic information
	assert.NotNil(t, plugin)
	assert.Equal(t, pluginName, plugin.Name())
	assert.Equal(t, pluginVersion, plugin.Version())
	assert.Equal(t, pluginDescription, plugin.Description())

	// Test configuration setting (direct setting, not through Configure method)
	validConfig := &conf.Polaris{
		Namespace: "test-namespace",
		Token:     "test-token",
		Weight:    100,
		Ttl:       30,
		Timeout:   &durationpb.Duration{Seconds: 10},
	}

	// Directly test configuration validation
	validator := NewValidator(validConfig)
	result := validator.Validate()
	assert.True(t, result.IsValid)
}

// TestPolarisPlugin_DefaultConfig tests default configuration settings
func TestPolarisPlugin_DefaultConfig(t *testing.T) {
	// Test default configuration values
	assert.Equal(t, "default", conf.DefaultNamespace)
	assert.Equal(t, int(100), conf.DefaultWeight)
	assert.Equal(t, int(30), conf.DefaultTTL)
	assert.NotNil(t, conf.GetDefaultTimeout())
}

// TestMetrics_Initialization tests monitoring metrics initialization
func TestMetrics_Initialization(t *testing.T) {
	metrics := NewPolarisMetrics()
	assert.NotNil(t, metrics)

	// Test metrics recording
	metrics.RecordSDKOperation("test-operation", "success")
	metrics.RecordServiceDiscovery("test-service", "test-namespace", "success")
	metrics.RecordHealthCheck("test-component", "success")
}

// TestRetryManager_Functionality tests retry manager functionality
func TestRetryManager_Functionality(t *testing.T) {
	retryManager := NewRetryManager(3, 1*time.Millisecond)
	assert.NotNil(t, retryManager)

	// Test successful operation
	successCount := 0
	err := retryManager.DoWithRetry(func() error {
		successCount++
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, successCount)

	// Test failure - exhaust retries
	attemptCount := 0
	err = retryManager.DoWithRetry(func() error {
		attemptCount++
		return assert.AnError
	})
	assert.Error(t, err)
	assert.Equal(t, 4, attemptCount) // 1 initial + 3 retries
}

// TestRetryManager_Context tests retry manager context support
func TestRetryManager_Context(t *testing.T) {
	retryManager := NewRetryManager(3, 1*time.Millisecond)
	assert.NotNil(t, retryManager)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := retryManager.DoWithRetryContext(ctx, func() error {
		return assert.AnError
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

// TestCircuitBreaker_Functionality tests circuit breaker functionality
func TestCircuitBreaker_Functionality(t *testing.T) {
	circuitBreaker := NewCircuitBreaker(0.5, 10*time.Millisecond)
	assert.NotNil(t, circuitBreaker)

	// Test successful operation
	err := circuitBreaker.Do(func() error { return nil })
	assert.NoError(t, err)
	assert.Equal(t, CircuitStateClosed, circuitBreaker.GetState())

	// Trigger failure: 1 success + 1 failure -> rate 0.5 >= 0.5, circuit opens
	_ = circuitBreaker.Do(func() error { return assert.AnError })
	// Next call should get "circuit breaker is open"
	err = circuitBreaker.Do(func() error { return nil })
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

// TestServiceWatcher_Functionality tests service watcher functionality
func TestServiceWatcher_Functionality(t *testing.T) {
	watcher := NewServiceWatcher(nil, "test-service", "test-namespace")
	assert.NotNil(t, watcher)

	// Test callback setting
	callbackCalled := false
	watcher.SetOnInstancesChanged(func(instances []model.Instance) {
		callbackCalled = true
	})

	watcher.SetOnError(func(err error) {
		// Error callback
	})

	// Verify callback setting success
	assert.False(t, callbackCalled)

	// Test start and stop
	watcher.Start()
	assert.True(t, watcher.IsRunning())

	watcher.Stop()
	assert.False(t, watcher.IsRunning())
}

// TestConfigWatcher_Functionality tests configuration watcher functionality
func TestConfigWatcher_Functionality(t *testing.T) {
	watcher := NewConfigWatcher(nil, "test-config", "test-group", "test-namespace")
	assert.NotNil(t, watcher)

	// Test callback setting
	callbackCalled := false
	watcher.SetOnConfigChanged(func(config model.ConfigFile) {
		callbackCalled = true
	})

	watcher.SetOnError(func(err error) {
		// Error callback
	})

	// Verify callback setting success
	assert.False(t, callbackCalled)

	// Test start and stop
	watcher.Start()
	assert.True(t, watcher.IsRunning())

	watcher.Stop()
	assert.False(t, watcher.IsRunning())
}

// TestValidator_NilConfig tests validator with nil config
func TestValidator_NilConfig(t *testing.T) {
	validator := NewValidator(nil)
	result := validator.Validate()
	assert.False(t, result.IsValid)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Error(), "configuration is required")
}

// TestValidator_Functionality tests configuration validator functionality
func TestValidator_Functionality(t *testing.T) {
	// Test valid configuration
	validConfig := &conf.Polaris{
		Namespace: "test-namespace",
		Weight:    100,
		Ttl:       30,
		Timeout:   &durationpb.Duration{Seconds: 10},
	}

	validator := NewValidator(validConfig)
	result := validator.Validate()
	assert.True(t, result.IsValid)
	assert.Empty(t, result.Errors)

	// Test invalid configuration
	invalidConfig := &conf.Polaris{
		Namespace: "", // Empty namespace
		Weight:    -1, // Invalid weight
		Ttl:       0,  // Invalid TTL
	}

	validator = NewValidator(invalidConfig)
	result = validator.Validate()
	assert.False(t, result.IsValid)
	assert.NotEmpty(t, result.Errors)
}

// TestPlugin_Integration tests plugin integration functionality
func TestPlugin_Integration(t *testing.T) {
	plugin := NewPolarisControlPlane()

	// Test state management
	assert.False(t, plugin.IsInitialized())
	assert.False(t, plugin.IsDestroyed())

	// Test service info setting
	serviceInfo := &ServiceInfo{
		Service:   "test-service",
		Namespace: "test-namespace",
		Host:      "localhost",
		Port:      8080,
		Protocol:  "http",
		Version:   "v1.0.0",
		Metadata:  map[string]string{"env": "test"},
	}

	plugin.SetServiceInfo(serviceInfo)
	retrievedInfo := plugin.GetServiceInfo()
	assert.Equal(t, serviceInfo, retrievedInfo)

	// Test monitoring metrics (should be nil when not initialized)
	metrics := plugin.GetMetrics()
	assert.Nil(t, metrics) // Should be nil when not initialized

	// Test service instance retrieval (uninitialized state)
	_, err := plugin.GetServiceInstances("test-service")
	assert.Error(t, err) // Should return error because not initialized
	assert.IsType(t, &PolarisError{}, err)

	// Test service watching (uninitialized state)
	_, err = plugin.WatchService("test-service")
	assert.Error(t, err) // Should return error because not initialized
	assert.IsType(t, &PolarisError{}, err)

	// Test configuration retrieval (uninitialized state)
	_, err = plugin.GetConfigValue("test-config", "test-group")
	assert.Error(t, err) // Should return error because not initialized
	assert.IsType(t, &PolarisError{}, err)

	// Test configuration watching (uninitialized state)
	_, err = plugin.WatchConfig("test-config", "test-group")
	assert.Error(t, err) // Should return error because not initialized
	assert.IsType(t, &PolarisError{}, err)

	// Test rate limit checking (uninitialized state)
	_, err = plugin.CheckRateLimit("test-service", map[string]string{"user": "test"})
	assert.Error(t, err) // Should return error because not initialized
	assert.IsType(t, &PolarisError{}, err)
}

// TestControlPlane_Interface tests control plane interface implementation
func TestControlPlane_Interface(t *testing.T) {
	plugin := NewPolarisControlPlane()

	// Test SystemCore interface
	namespace := plugin.GetNamespace()
	assert.Equal(t, "default", namespace) // Default namespace

	// Test RateLimiter interface
	httpRateLimit := plugin.HTTPRateLimit()
	assert.Nil(t, httpRateLimit) // Returns nil when not initialized

	grpcRateLimit := plugin.GRPCRateLimit()
	assert.Nil(t, grpcRateLimit) // Returns nil when not initialized

	// Test ServiceRegistry interface
	registrar := plugin.NewServiceRegistry()
	assert.Nil(t, registrar) // Returns nil when not initialized

	discovery := plugin.NewServiceDiscovery()
	assert.Nil(t, discovery) // Returns nil when not initialized

	// Test RouteManager interface
	nodeFilter := plugin.NewNodeRouter("test-service")
	assert.Nil(t, nodeFilter) // Returns nil when not initialized

	// Test ConfigManager interface
	configSource, err := plugin.GetConfig("test-config", "test-group")
	assert.NoError(t, err)
	assert.Nil(t, configSource) // Returns nil when not initialized
}

// TestErrorHandling tests error handling
func TestErrorHandling(t *testing.T) {
	plugin := NewPolarisControlPlane()

	// Test operations in uninitialized state
	_, err := plugin.GetServiceInstances("test-service")
	assert.Error(t, err)
	assert.IsType(t, &PolarisError{}, err)

	_, err = plugin.WatchService("test-service")
	assert.Error(t, err)
	assert.IsType(t, &PolarisError{}, err)

	_, err = plugin.GetConfigValue("test-config", "test-group")
	assert.Error(t, err)
	assert.IsType(t, &PolarisError{}, err)

	_, err = plugin.WatchConfig("test-config", "test-group")
	assert.Error(t, err)
	assert.IsType(t, &PolarisError{}, err)

	_, err = plugin.CheckRateLimit("test-service", nil)
	assert.Error(t, err)
	assert.IsType(t, &PolarisError{}, err)
}

// TestPlugin_HealthCheck tests health check
func TestPlugin_HealthCheck(t *testing.T) {
	plugin := NewPolarisControlPlane()

	// Test uninitialized state
	err := plugin.CheckHealth()
	assert.Error(t, err)
	assert.IsType(t, &PolarisError{}, err)
}

// BenchmarkRetryManager retry manager performance test
func BenchmarkRetryManager(b *testing.B) {
	retryManager := NewRetryManager(3, 1*time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = retryManager.DoWithRetry(func() error {
			return nil
		})
	}
}

// BenchmarkCircuitBreaker circuit breaker performance test
func BenchmarkCircuitBreaker(b *testing.B) {
	circuitBreaker := NewCircuitBreaker(0.5, 30*time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = circuitBreaker.Do(func() error {
			return nil
		})
	}
}
