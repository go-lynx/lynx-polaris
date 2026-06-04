package polaris

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kratos/kratos/contrib/polaris/v2"
	"github.com/go-lynx/lynx"
	"github.com/go-lynx/lynx-polaris/conf"
	"github.com/go-lynx/lynx/log"
	"github.com/go-lynx/lynx/plugins"
	"github.com/polarismesh/polaris-go/api"
	"github.com/polarismesh/polaris-go/pkg/model"
	"google.golang.org/protobuf/proto"
)

// Plugin metadata
// Plugin metadata defining basic plugin information
const (
	// pluginName is the unique identifier for the Polaris control plane plugin, used to identify this plugin in the plugin system.
	pluginName = "polaris.control.plane"

	// pluginVersion represents the current version of the Polaris control plane plugin.
	pluginVersion = "v1.6.1"

	// pluginDescription briefly describes the functionality of the Polaris control plane plugin.
	pluginDescription = "polaris control plane plugin for lynx framework"

	// confPrefix is the configuration prefix used when loading Polaris configuration.
	confPrefix = "lynx.polaris"
)

// PlugPolaris represents a Polaris control plane plugin instance
type PlugPolaris struct {
	*plugins.BasePlugin
	polaris *polaris.Polaris
	conf    *conf.Polaris
	rt      plugins.Runtime

	// SDK components
	sdk api.SDKContext

	// Handed-out registry adapters that wrap the same SDK context. Retained so
	// they can be torn down (deregistered) before the SDK is destroyed, avoiding
	// use-after-destroy when Kratos calls Register/GetService during shutdown.
	registrar *PolarisRegistrar

	// Enhanced components
	metrics        *Metrics
	retryManager   *RetryManager
	circuitBreaker *CircuitBreaker

	// State management - using atomic operations to improve concurrency safety
	mu            sync.RWMutex
	initialized   int32 // Use int32 instead of bool to support atomic operations
	destroyed     int32 // Use int32 instead of bool to support atomic operations
	healthCheckCh chan struct{}
	lifecycleCtx  context.Context
	lifecycleStop context.CancelFunc

	// Service information
	serviceInfo *ServiceInfo

	// Event handling
	activeWatchers map[string]*ServiceWatcher // Active service watchers
	configWatchers map[string]*ConfigWatcher  // Active configuration watchers
	watcherMutex   sync.RWMutex               // Watcher mutex

	// Retry deduplication: prevent multiple retry goroutines for same service/config
	retryingServiceWatchers map[string]struct{}
	retryingConfigWatchers  map[string]struct{}
	retryMutex              sync.Mutex

	// Cache system
	serviceCache map[string]any // Service instance cache
	configCache  map[string]any // Configuration cache
	cacheMutex   sync.RWMutex   // Cache mutex
}

// ServiceInfo service registration information
type ServiceInfo struct {
	Service   string            `json:"service"`
	Namespace string            `json:"namespace"`
	Host      string            `json:"host"`
	Port      int32             `json:"port"`
	Protocol  string            `json:"protocol"`
	Version   string            `json:"version"`
	Metadata  map[string]string `json:"metadata"`
}

// NewPolarisControlPlane creates a new Polaris control plane.
// This function initializes the plugin's basic information and returns a pointer to PlugPolaris.
func NewPolarisControlPlane() *PlugPolaris {
	return &PlugPolaris{
		BasePlugin: plugins.NewBasePlugin(
			// Generate unique plugin ID
			plugins.GeneratePluginID("", pluginName, pluginVersion),
			// Plugin name
			pluginName,
			// Plugin description
			pluginDescription,
			// Plugin version
			pluginVersion,
			// Configuration prefix
			confPrefix,
			// Weight
			math.MaxInt,
		),
		healthCheckCh:           make(chan struct{}),
		activeWatchers:          make(map[string]*ServiceWatcher),
		configWatchers:          make(map[string]*ConfigWatcher),
		retryingServiceWatchers: make(map[string]struct{}),
		retryingConfigWatchers:  make(map[string]struct{}),
		serviceCache:            make(map[string]any),
		configCache:             make(map[string]any),
	}
}

// InitializeResources implements custom initialization logic for the Polaris plugin.
// This function loads and validates Polaris configuration, using default configuration if none is provided.
func (p *PlugPolaris) InitializeResources(rt plugins.Runtime) error {
	p.rt = rt
	// Initialize an empty configuration structure
	p.conf = &conf.Polaris{}

	// Scan and load Polaris configuration from runtime configuration
	err := rt.GetConfig().Value(confPrefix).Scan(p.conf)
	if err != nil {
		return WrapInitError(err, "failed to scan polaris configuration")
	}

	// Set default configuration
	p.setDefaultConfig()

	// Validate configuration
	if err := p.validateConfig(); err != nil {
		return WrapInitError(err, "configuration validation failed")
	}

	// Initialize enhanced components
	if err := p.initComponents(); err != nil {
		return WrapInitError(err, "failed to initialize components")
	}

	return nil
}

// setDefaultConfig sets default configuration
func (p *PlugPolaris) setDefaultConfig() {
	// Default namespace is 'default'
	if p.conf.Namespace == "" {
		p.conf.Namespace = conf.DefaultNamespace
	}
	// Default service instance weight is 100
	if p.conf.Weight == 0 {
		p.conf.Weight = conf.DefaultWeight
	}
	// Default TTL is 5 seconds
	if p.conf.Ttl == 0 {
		p.conf.Ttl = conf.DefaultTTL
	}
	// Default timeout is 5 seconds
	if p.conf.Timeout == nil {
		p.conf.Timeout = conf.GetDefaultTimeout()
	}
	// Default shutdown timeout for graceful cleanup
	if p.conf.ShutdownTimeout == nil {
		p.conf.ShutdownTimeout = conf.GetDefaultShutdownTimeout()
	}
	// Default circuit breaker threshold
	if p.conf.CircuitBreakerThreshold <= 0 {
		p.conf.CircuitBreakerThreshold = float32(conf.DefaultCircuitBreakerThreshold)
	}
}

// validateConfig validates configuration
func (p *PlugPolaris) validateConfig() error {
	if p.conf == nil {
		return NewConfigError("configuration is required")
	}

	validator := NewValidator(p.conf)
	result := validator.Validate()
	if !result.IsValid {
		return NewConfigError(result.Errors[0].Error())
	}

	return nil
}

// initComponents initializes enhanced components
func (p *PlugPolaris) initComponents() error {
	// Initialize monitoring metrics
	p.metrics = NewPolarisMetrics()

	// Initialize retry manager from config
	maxRetry := int(p.conf.MaxRetryTimes)
	if maxRetry <= 0 {
		maxRetry = 3
	}
	retryInterval := time.Second
	if p.conf.RetryInterval != nil && p.conf.RetryInterval.AsDuration() > 0 {
		retryInterval = p.conf.RetryInterval.AsDuration()
	} else {
		retryInterval = conf.DefaultRetryInterval
	}
	p.retryManager = NewRetryManager(maxRetry, retryInterval)

	// Initialize circuit breaker from config (threshold + half-open timeout from defaults)
	threshold := float64(p.conf.CircuitBreakerThreshold)
	if threshold <= 0 {
		threshold = conf.DefaultCircuitBreakerThreshold
	}
	halfOpenTimeout := conf.DefaultCircuitBreakerHalfOpenTimeout
	p.circuitBreaker = NewCircuitBreaker(threshold, halfOpenTimeout)

	return nil
}

// checkInitialized unified state checking method ensuring thread safety
func (p *PlugPolaris) checkInitialized() error {
	if atomic.LoadInt32(&p.initialized) == 0 {
		return NewInitError("Polaris plugin not initialized")
	}
	if atomic.LoadInt32(&p.destroyed) == 1 {
		return NewInitError("Polaris plugin has been destroyed")
	}
	return nil
}

// setInitialized atomically sets initialization status
func (p *PlugPolaris) setInitialized() {
	atomic.StoreInt32(&p.initialized, 1)
}

func (p *PlugPolaris) clearInitialized() {
	atomic.StoreInt32(&p.initialized, 0)
}

// setDestroyed atomically sets destruction status
func (p *PlugPolaris) setDestroyed() {
	atomic.StoreInt32(&p.destroyed, 1)
}

// StartupTasks implements custom startup logic for the Polaris plugin.
// This function configures and starts the Polaris control plane, adding necessary middleware and configuration options.
func (p *PlugPolaris) StartupTasks() error {
	return p.startupTasksContext(context.Background())
}

func (p *PlugPolaris) publishRuntimeResources() error {
	if p.rt == nil {
		return nil
	}
	if err := lynx.RegisterControlPlaneCapabilityResources(p.rt, pluginName, p); err != nil {
		return err
	}
	if registrar := p.NewServiceRegistry(); registrar != nil {
		// Retain the concrete registrar so it can be deregistered before the SDK
		// context is destroyed during cleanup.
		if pr, ok := registrar.(*PolarisRegistrar); ok {
			p.mu.Lock()
			p.registrar = pr
			p.mu.Unlock()
		}
		if err := p.rt.RegisterSharedResource(pluginName+".service_registry", registrar); err != nil {
			log.Warnf("failed to register polaris service registry resource: %v", err)
		}
	}
	if discovery := p.NewServiceDiscovery(); discovery != nil {
		if err := p.rt.RegisterSharedResource(pluginName+".service_discovery", discovery); err != nil {
			log.Warnf("failed to register polaris service discovery resource: %v", err)
		}
	}
	if router := p.NewNodeRouter(currentLynxName()); router != nil {
		if err := p.rt.RegisterSharedResource(pluginName+".router", router); err != nil {
			log.Warnf("failed to register polaris router resource: %v", err)
		}
	}
	if p.sdk != nil {
		if err := p.rt.RegisterPrivateResource("polaris_sdk", p.sdk); err != nil {
			log.Warnf("failed to register polaris private sdk alias resource: %v", err)
		}
		if err := p.rt.RegisterPrivateResource("sdk", p.sdk); err != nil {
			log.Warnf("failed to register polaris private sdk resource: %v", err)
		}
	}
	if p.polaris != nil {
		if err := p.rt.RegisterPrivateResource("polaris_client", p.polaris); err != nil {
			log.Warnf("failed to register polaris private client alias resource: %v", err)
		}
		if err := p.rt.RegisterPrivateResource("client", p.polaris); err != nil {
			log.Warnf("failed to register polaris private client resource: %v", err)
		}
	}
	return nil
}

// GetMetrics gets monitoring metrics
func (p *PlugPolaris) GetMetrics() *Metrics {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.metrics
}

// IsInitialized checks if initialized
func (p *PlugPolaris) IsInitialized() bool {
	return atomic.LoadInt32(&p.initialized) == 1
}

// IsDestroyed checks if destroyed
func (p *PlugPolaris) IsDestroyed() bool {
	return atomic.LoadInt32(&p.destroyed) == 1
}

// GetPolarisConfig gets Polaris configuration
func (p *PlugPolaris) GetPolarisConfig() *conf.Polaris {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.conf == nil {
		return nil
	}
	return proto.Clone(p.conf).(*conf.Polaris)
}

// SetServiceInfo sets service information
func (p *PlugPolaris) SetServiceInfo(info *ServiceInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.serviceInfo = cloneServiceInfo(info)
}

// GetServiceInfo gets service information
func (p *PlugPolaris) GetServiceInfo() *ServiceInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return cloneServiceInfo(p.serviceInfo)
}

func cloneServiceInfo(info *ServiceInfo) *ServiceInfo {
	if info == nil {
		return nil
	}
	clone := *info
	if info.Metadata != nil {
		clone.Metadata = make(map[string]string, len(info.Metadata))
		for k, v := range info.Metadata {
			clone.Metadata[k] = v
		}
	}
	return &clone
}

// ControlPlaneCapabilities declares Polaris' explicit control plane contract.
func (p *PlugPolaris) ControlPlaneCapabilities() []lynx.ControlPlaneCapability {
	return []lynx.ControlPlaneCapability{
		lynx.ControlPlaneCapabilityConfig,
		lynx.ControlPlaneCapabilityWatcher,
		lynx.ControlPlaneCapabilityRegistry,
		lynx.ControlPlaneCapabilityDiscovery,
		lynx.ControlPlaneCapabilityRouter,
		lynx.ControlPlaneCapabilityRateLimit,
	}
}

// WatchConfig watches configuration changes
func (p *PlugPolaris) WatchConfig(fileName, group string) (*ConfigWatcher, error) {
	if !p.IsInitialized() {
		return nil, NewInitError("Polaris plugin not initialized")
	}

	// Record configuration watch operation metrics
	if p.metrics != nil {
		p.metrics.RecordSDKOperation("watch_config", "start")
		defer func() {
			if p.metrics != nil {
				p.metrics.RecordSDKOperation("watch_config", "success")
			}
		}()
	}

	log.Infof("Watching config: %s, group: %s", fileName, group)

	// Snapshot mutable plugin state under the lock to avoid a data race / nil
	// dereference if cleanup runs concurrently.
	p.mu.RLock()
	sdk := p.sdk
	namespace := ""
	if p.conf != nil {
		namespace = p.conf.Namespace
	}
	metrics := p.metrics
	p.mu.RUnlock()

	if sdk == nil {
		return nil, NewInitError("Polaris plugin has been destroyed")
	}

	// Check if the configuration is already being watched
	configKey := fmt.Sprintf("%s:%s", fileName, group)
	p.watcherMutex.Lock()
	if existingWatcher, exists := p.configWatchers[configKey]; exists {
		p.watcherMutex.Unlock()
		log.Infof("Config %s:%s is already being watched", fileName, group)
		return existingWatcher, nil
	}
	p.watcherMutex.Unlock()

	// Create Config API client
	configAPI := api.NewConfigFileAPIBySDKContext(sdk)
	if configAPI == nil {
		return nil, NewInitError("failed to create config API")
	}

	// Create configuration watcher and connect to SDK
	watcher := NewConfigWatcherWithContext(p.watcherContext(), configAPI, fileName, group, namespace)
	watcher.metrics = metrics // Pass metrics reference

	// Set event handling callbacks
	watcher.SetOnConfigChanged(func(config model.ConfigFile) {
		p.handleConfigChanged(fileName, group, config)
	})

	watcher.SetOnError(func(err error) {
		p.handleConfigWatchError(fileName, group, err)
	})

	// Register watcher
	p.watcherMutex.Lock()
	p.configWatchers[configKey] = watcher
	p.watcherMutex.Unlock()

	// Start watching
	watcher.Start()

	return watcher, nil
}

// recordServiceChangeAudit logs an audit entry for a service-instance change event.
func (p *PlugPolaris) recordServiceChangeAudit(serviceName string, instances []model.Instance) {
	type instanceEntry struct {
		ID       string `json:"id"`
		Host     string `json:"host"`
		Port     uint32 `json:"port"`
		Weight   int    `json:"weight"`
		Healthy  bool   `json:"healthy"`
		Isolated bool   `json:"isolated"`
	}
	entries := make([]instanceEntry, 0, len(instances))
	for _, inst := range instances {
		if inst == nil {
			continue
		}
		entries = append(entries, instanceEntry{
			ID:       inst.GetId(),
			Host:     inst.GetHost(),
			Port:     inst.GetPort(),
			Weight:   inst.GetWeight(),
			Healthy:  inst.IsHealthy(),
			Isolated: inst.IsIsolated(),
		})
	}
	log.Infof("Service change audit: service=%s namespace=%s count=%d instances=%+v",
		serviceName, p.conf.Namespace, len(instances), entries)
}

// recordServiceWatchErrorAudit logs an audit entry for a service-watcher error.
func (p *PlugPolaris) recordServiceWatchErrorAudit(serviceName string, err error) {
	if p.conf == nil {
		return
	}
	log.Errorf("Service watch error audit: service=%s namespace=%s initialized=%v destroyed=%v err=%v errType=%T",
		serviceName, p.conf.Namespace, p.IsInitialized(), p.IsDestroyed(), err, err)
}

// sendServiceWatchAlert emits a structured warning log for a service-watcher error.
// Integrate external alerting (PagerDuty, DingTalk, SMS, etc.) here when needed.
func (p *PlugPolaris) sendServiceWatchAlert(serviceName string, err error) {
	if p.conf == nil {
		return
	}
	log.Warnf("Service watch alert: type=service_watch_error service=%s namespace=%s severity=warning initialized=%v destroyed=%v err=%v",
		serviceName, p.conf.Namespace, p.IsInitialized(), p.IsDestroyed(), err)
}

// recordConfigChangeAudit logs an audit entry for a configuration change event.
func (p *PlugPolaris) recordConfigChangeAudit(fileName, group string, config model.ConfigFile) {
	if p.conf == nil || config == nil {
		return
	}
	log.Infof("Config change audit: file=%s group=%s namespace=%s contentLen=%d changeType=config_updated",
		fileName, group, p.conf.Namespace, len(config.GetContent()))
}

// recordConfigWatchErrorAudit logs an audit entry for a config-watcher error.
func (p *PlugPolaris) recordConfigWatchErrorAudit(fileName, group string, err error) {
	if p.conf == nil {
		return
	}
	log.Errorf("Config watch error audit: file=%s group=%s namespace=%s initialized=%v destroyed=%v err=%v errType=%T",
		fileName, group, p.conf.Namespace, p.IsInitialized(), p.IsDestroyed(), err, err)
}

// sendConfigWatchAlert emits a structured warning log for a config-watcher error.
// Integrate external alerting here when needed.
func (p *PlugPolaris) sendConfigWatchAlert(fileName, group string, err error) {
	if p.conf == nil {
		return
	}
	log.Warnf("Config watch alert: type=config_watch_error file=%s group=%s namespace=%s severity=warning err=%v",
		fileName, group, p.conf.Namespace, err)
}

// tryStartConfigWatchRetry marks config as retrying and returns true if this goroutine should run the retry.
// Returns false if a retry is already in progress for this config (deduplication).
func (p *PlugPolaris) tryStartConfigWatchRetry(configKey string) bool {
	p.retryMutex.Lock()
	defer p.retryMutex.Unlock()
	if p.retryingConfigWatchers == nil {
		return false
	}
	if _, exists := p.retryingConfigWatchers[configKey]; exists {
		return false
	}
	p.retryingConfigWatchers[configKey] = struct{}{}
	return true
}

// finishConfigWatchRetry removes config from retrying set (call when retry completes).
func (p *PlugPolaris) finishConfigWatchRetry(fileName, group string) {
	configKey := fmt.Sprintf("%s:%s", fileName, group)
	p.retryMutex.Lock()
	defer p.retryMutex.Unlock()
	if p.retryingConfigWatchers != nil {
		delete(p.retryingConfigWatchers, configKey)
	}
}

// retryConfigWatch retries configuration watching
func (p *PlugPolaris) retryConfigWatch(fileName, group string) {
	defer p.finishConfigWatchRetry(fileName, group)
	log.Infof("Retrying config watch for %s:%s", fileName, group)

	if p.waitForRetryDelay(5 * time.Second) {
		log.Infof("Config watch retry canceled due to plugin shutdown: %s:%s", fileName, group)
		return
	}

	if p.IsDestroyed() {
		return
	}

	// Recreate watcher
	if _, err := p.WatchConfig(fileName, group); err == nil {
		log.Infof("Successfully recreated config watcher for %s:%s", fileName, group)
	} else {
		log.Errorf("Failed to recreate config watcher for %s:%s: %v", fileName, group, err)
	}
}
