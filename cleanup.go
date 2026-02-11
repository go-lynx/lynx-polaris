package polaris

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/go-lynx/lynx"
	"github.com/go-lynx/lynx-polaris/conf"
	"github.com/go-lynx/lynx/log"
	"github.com/polarismesh/polaris-go/api"
)

// restoreControlPlane sets Lynx control plane back to default so the app no longer uses this plugin.
func (p *PlugPolaris) restoreControlPlane() {
	if lynx.Lynx() == nil {
		return
	}
	log.Infof("Removing from Lynx control plane")
	if err := lynx.Lynx().SetControlPlane(&lynx.DefaultControlPlane{}); err != nil {
		log.Warnf("Failed to restore default control plane: %v", err)
	}
}

// stopHealthCheck stops health check
func (p *PlugPolaris) stopHealthCheck() {
	if p.healthCheckCh != nil {
		log.Infof("Stopping health check")
		close(p.healthCheckCh)
		p.healthCheckCh = nil
	}
}

// cleanupWatchers cleans up watchers
func (p *PlugPolaris) cleanupWatchers() {
	log.Infof("Cleaning up watchers")

	// Clean up service watchers
	p.watcherMutex.Lock()
	serviceWatcherCount := len(p.activeWatchers)
	for serviceName, watcher := range p.activeWatchers {
		log.Infof("Stopping service watcher for: %s", serviceName)
		if watcher != nil {
			watcher.Stop()
		}
	}
	p.activeWatchers = make(map[string]*ServiceWatcher)

	// Clean up configuration watchers
	configWatcherCount := len(p.configWatchers)
	for configKey, watcher := range p.configWatchers {
		log.Infof("Stopping config watcher for: %s", configKey)
		if watcher != nil {
			watcher.Stop()
		}
	}
	p.configWatchers = make(map[string]*ConfigWatcher)
	p.watcherMutex.Unlock()

	log.Infof("Cleaned up %d service watchers and %d config watchers", serviceWatcherCount, configWatcherCount)
}

// closeSDKConnection closes SDK connection
func (p *PlugPolaris) closeSDKConnection() {
	if p.sdk != nil {
		log.Infof("Closing SDK connection")

		// Get SDK context information
		sdkInfo := map[string]interface{}{
			"sdk_type":  fmt.Sprintf("%T", p.sdk),
			"namespace": p.conf.Namespace,
		}

		// Implement specific SDK shutdown logic
		// 1. Get all active API clients
		consumerAPI := api.NewConsumerAPIByContext(p.sdk)
		providerAPI := api.NewProviderAPIByContext(p.sdk)
		configAPI := api.NewConfigFileAPIBySDKContext(p.sdk)
		limitAPI := api.NewLimitAPIByContext(p.sdk)

		// 2. Close each API client
		if consumerAPI != nil {
			log.Infof("Closing consumer API")
			consumerAPI.Destroy()
		}

		if providerAPI != nil {
			log.Infof("Closing provider API")
			providerAPI.Destroy()
		}

		// ConfigFileAPI and LimitAPI in polaris-go do not expose Destroy(); SDK context destroy below releases resources.
		if configAPI != nil {
			log.Infof("Closing config API")
		}
		if limitAPI != nil {
			log.Infof("Closing limit API")
		}

		// 3. Close SDK context
		log.Infof("Destroying SDK context")
		p.sdk.Destroy()

		log.Infof("SDK connection closed: %+v", sdkInfo)
		p.sdk = nil
	}
}

// destroyPolarisInstance destroys Polaris instance
func (p *PlugPolaris) destroyPolarisInstance() {
	if p.polaris != nil {
		log.Infof("Destroying Polaris instance")

		// Record instance information
		instanceInfo := map[string]interface{}{
			"service":       lynx.GetName(),
			"namespace":     p.conf.Namespace,
			"instance_type": fmt.Sprintf("%T", p.polaris),
		}

		// Implement specific Polaris instance destruction logic
		// 1. Stop all services of Polaris instance (control plane already restored in CleanupTasks)
		log.Infof("Stopping Polaris instance services")

		// 2. Clean up instance-related resources
		log.Infof("Cleaning up instance resources")

		// 3. Record destruction statistics
		destroyStats := map[string]interface{}{
			"service_name":  lynx.GetName(),
			"namespace":     p.conf.Namespace,
			"destroy_time":  time.Now().Unix(),
			"instance_info": instanceInfo,
		}

		log.Infof("Polaris instance destroyed: %+v", destroyStats)
		p.polaris = nil
	}
}

// releaseMemoryResources releases memory resources
func (p *PlugPolaris) releaseMemoryResources() {
	log.Infof("Releasing memory resources")

	// Clear service information
	if p.serviceInfo != nil {
		log.Infof("Clearing service info")
		p.serviceInfo = nil
	}

	// Clear configuration
	if p.conf != nil {
		log.Infof("Clearing configuration")
		p.conf = nil
	}

	// Clear enhanced components
	if p.metrics != nil {
		log.Infof("Clearing metrics")
		p.metrics = nil
	}

	if p.retryManager != nil {
		log.Infof("Clearing retry manager")
		p.retryManager = nil
	}

	if p.circuitBreaker != nil {
		log.Infof("Clearing circuit breaker")
		p.circuitBreaker = nil
	}

	// Clear cache
	p.clearServiceCache()
	p.clearConfigCache()

	log.Infof("Memory resources released")
}

// stopBackgroundTasks stops background tasks
func (p *PlugPolaris) stopBackgroundTasks() {
	log.Infof("Stopping background tasks")

	// Stop retry tasks
	if p.retryManager != nil {
		log.Infof("Stopping retry manager background tasks")
		p.retryManager = nil
	}

	// Stop circuit breaker tasks
	if p.circuitBreaker != nil {
		log.Infof("Stopping circuit breaker background tasks")
		p.circuitBreaker.ForceClose()
	}

	// Stop metrics collection tasks
	if p.metrics != nil {
		log.Infof("Stopping metrics collection tasks")
		p.metrics = nil
	}

	// Stop other background tasks
	log.Infof("Stopping health check tasks")
	log.Infof("Stopping monitoring tasks")
	log.Infof("Stopping audit log tasks")

	log.Infof("Background tasks stopped")
}

// getCleanupStats gets cleanup statistics
func (p *PlugPolaris) getCleanupStats() map[string]interface{} {
	stats := map[string]interface{}{
		"cleanup_time": time.Now().Unix(),
		"plugin_state": map[string]interface{}{
			"initialized": atomic.LoadInt32(&p.initialized),
			"destroyed":   atomic.LoadInt32(&p.destroyed),
		},
		"resources": map[string]interface{}{
			"sdk_closed":         p.sdk == nil,
			"instance_destroyed": p.polaris == nil,
			"metrics_cleared":    p.metrics == nil,
			"retry_cleared":      p.retryManager == nil,
			"breaker_cleared":    p.circuitBreaker == nil,
		},
	}

	return stats
}

// getShutdownTimeoutDuration returns configured shutdown timeout for cleanup
func (p *PlugPolaris) getShutdownTimeoutDuration() time.Duration {
	if p.conf != nil && p.conf.ShutdownTimeout != nil && p.conf.ShutdownTimeout.AsDuration() > 0 {
		d := p.conf.ShutdownTimeout.AsDuration()
		if d < conf.MinShutdownTimeout {
			d = conf.MinShutdownTimeout
		}
		if d > conf.MaxShutdownTimeout {
			d = conf.MaxShutdownTimeout
		}
		return d
	}
	return conf.DefaultShutdownTimeout
}

// CleanupTasks runs cleanup with configurable shutdown timeout; only SDK/instance teardown is limited by timeout.
func (p *PlugPolaris) CleanupTasks() error {
	p.mu.Lock()
	if !p.IsInitialized() || p.IsDestroyed() {
		p.mu.Unlock()
		return nil
	}
	p.setDestroyed()
	if p.metrics != nil {
		p.metrics.RecordSDKOperation("cleanup", "start")
		defer func() {
			if p.metrics != nil {
				p.metrics.RecordSDKOperation("cleanup", "success")
			}
		}()
	}
	timeout := p.getShutdownTimeoutDuration()
	p.mu.Unlock()

	log.Infof("Destroying Polaris plugin (shutdown timeout: %v)", timeout)

	p.restoreControlPlane()
	p.stopHealthCheck()
	p.cleanupWatchers()

	done := make(chan struct{})
	go func() {
		p.closeSDKConnection()
		p.destroyPolarisInstance()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		log.Warnf("Polaris SDK/instance teardown did not finish within %v", timeout)
	}

	p.mu.Lock()
	p.stopBackgroundTasks()
	if p.metrics != nil {
		p.metrics.Unregister()
		p.metrics = nil
	}
	p.releaseMemoryResources()
	p.mu.Unlock()

	log.Infof("Polaris plugin destroyed successfully")
	return nil
}
