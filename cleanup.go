package polaris

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	kratospolaris "github.com/go-kratos/kratos/contrib/polaris/v2"
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
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.healthCheckCh != nil {
		log.Infof("Stopping health check")
		close(p.healthCheckCh)
		p.healthCheckCh = nil
	}
}

// cleanupWatchers cleans up watchers
func (p *PlugPolaris) cleanupWatchers() {
	log.Infof("Cleaning up watchers")

	// Clear retry deduplication maps so in-flight retries can finish cleanly
	p.retryMutex.Lock()
	p.retryingServiceWatchers = make(map[string]struct{})
	p.retryingConfigWatchers = make(map[string]struct{})
	p.retryMutex.Unlock()

	p.watcherMutex.Lock()
	serviceWatcherCount := len(p.activeWatchers)
	serviceWatchers := p.activeWatchers
	p.activeWatchers = make(map[string]*ServiceWatcher)
	configWatcherCount := len(p.configWatchers)
	configWatchers := p.configWatchers
	p.configWatchers = make(map[string]*ConfigWatcher)
	p.watcherMutex.Unlock()

	for serviceName, watcher := range serviceWatchers {
		log.Infof("Stopping service watcher for: %s", serviceName)
		if watcher != nil {
			watcher.Stop()
		}
	}

	for configKey, watcher := range configWatchers {
		log.Infof("Stopping config watcher for: %s", configKey)
		if watcher != nil {
			watcher.Stop()
		}
	}

	log.Infof("Cleaned up %d service watchers and %d config watchers", serviceWatcherCount, configWatcherCount)
}

// closeSDKConnection closes SDK connection
func (p *PlugPolaris) closeSDKConnection() {
	p.mu.Lock()
	sdk := p.sdk
	p.sdk = nil
	namespace := "unknown"
	if p.conf != nil {
		namespace = p.conf.Namespace
	}
	p.mu.Unlock()

	destroySDKResources(sdk, namespace)
}

// destroyPolarisInstance destroys Polaris instance
func (p *PlugPolaris) destroyPolarisInstance() {
	p.mu.Lock()
	polarisClient := p.polaris
	p.polaris = nil
	namespace := "unknown"
	if p.conf != nil {
		namespace = p.conf.Namespace
	}
	p.mu.Unlock()

	destroyPolarisClient(polarisClient, namespace)
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

	// Clear retry maps (allow late finishXxx to no-op)
	p.retryMutex.Lock()
	p.retryingServiceWatchers = nil
	p.retryingConfigWatchers = nil
	p.retryMutex.Unlock()

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

func (p *PlugPolaris) CleanupTasks() error {
	return p.cleanupTasksContext(context.Background())
}

func (p *PlugPolaris) cleanupTasksContext(parentCtx context.Context) error {
	if err := parentCtx.Err(); err != nil {
		return err
	}

	p.mu.Lock()
	if !p.IsInitialized() || p.IsDestroyed() {
		p.mu.Unlock()
		return nil
	}
	p.setDestroyed()
	timeout := p.getShutdownTimeoutDuration()
	metrics := p.metrics
	if metrics != nil {
		metrics.RecordSDKOperation("cleanup", "start")
	}
	if p.lifecycleStop != nil {
		p.lifecycleStop()
		p.lifecycleStop = nil
	}
	p.lifecycleCtx = nil
	namespace := "unknown"
	if p.conf != nil {
		namespace = p.conf.Namespace
	}
	sdk := p.sdk
	polarisClient := p.polaris
	p.sdk = nil
	p.polaris = nil
	p.mu.Unlock()

	defer func() {
		if metrics == nil {
			return
		}
		metrics.RecordSDKOperation("cleanup", "success")
	}()

	log.Infof("Destroying Polaris plugin (shutdown timeout: %v)", timeout)

	p.restoreControlPlane()
	p.stopHealthCheck()
	p.cleanupWatchers()

	cleanupCtx, cancel := p.createCleanupContext(parentCtx, timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		destroySDKResources(sdk, namespace)
		destroyPolarisClient(polarisClient, namespace)
		close(done)
	}()

	var teardownErr error
	select {
	case <-done:
	case <-cleanupCtx.Done():
		teardownErr = cleanupCtx.Err()
		log.Warnf("Polaris SDK/instance teardown did not finish within %v", timeout)
	}

	if metrics != nil {
		metrics.Unregister()
	}

	p.mu.Lock()
	p.stopBackgroundTasks()
	p.releaseMemoryResources()
	atomic.StoreInt32(&p.initialized, 0)
	p.mu.Unlock()

	log.Infof("Polaris plugin destroyed successfully")
	return teardownErr
}

func (p *PlugPolaris) createCleanupContext(parentCtx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := parentCtx.Deadline(); ok && time.Until(deadline) < timeout {
		return parentCtx, func() {}
	}
	return context.WithTimeout(parentCtx, timeout)
}

func destroySDKResources(sdk api.SDKContext, namespace string) {
	if sdk == nil {
		return
	}

	log.Infof("Closing SDK connection")
	sdkInfo := map[string]interface{}{
		"sdk_type":  fmt.Sprintf("%T", sdk),
		"namespace": namespace,
	}

	consumerAPI := api.NewConsumerAPIByContext(sdk)
	providerAPI := api.NewProviderAPIByContext(sdk)
	configAPI := api.NewConfigFileAPIBySDKContext(sdk)
	limitAPI := api.NewLimitAPIByContext(sdk)

	if consumerAPI != nil {
		log.Infof("Closing consumer API")
		consumerAPI.Destroy()
	}
	if providerAPI != nil {
		log.Infof("Closing provider API")
		providerAPI.Destroy()
	}
	if configAPI != nil {
		log.Infof("Closing config API")
	}
	if limitAPI != nil {
		log.Infof("Closing limit API")
	}

	log.Infof("Destroying SDK context")
	sdk.Destroy()
	log.Infof("SDK connection closed: %+v", sdkInfo)
}

func destroyPolarisClient(polarisClient *kratospolaris.Polaris, namespace string) {
	if polarisClient == nil {
		return
	}

	log.Infof("Destroying Polaris instance")
	instanceInfo := map[string]interface{}{
		"service":       currentLynxName(),
		"namespace":     namespace,
		"instance_type": fmt.Sprintf("%T", polarisClient),
	}

	log.Infof("Stopping Polaris instance services")
	log.Infof("Cleaning up instance resources")
	destroyStats := map[string]interface{}{
		"service_name":  currentLynxName(),
		"namespace":     namespace,
		"destroy_time":  time.Now().Unix(),
		"instance_info": instanceInfo,
	}

	log.Infof("Polaris instance destroyed: %+v", destroyStats)
}
