package polaris

import (
	"time"

	"github.com/polarismesh/polaris-go/api"

	"github.com/go-lynx/lynx/log"
	"github.com/polarismesh/polaris-go/pkg/model"
)

// GetServiceInstances gets service instances
func (p *PlugPolaris) GetServiceInstances(serviceName string) ([]model.Instance, error) {
	if err := p.checkInitialized(); err != nil {
		return nil, err
	}

	// Snapshot sdk/namespace/metrics/breaker under the lock to avoid a data race
	// and nil-pointer panic if cleanup runs concurrently with this request.
	p.mu.RLock()
	sdk := p.sdk
	namespace := ""
	if p.conf != nil {
		namespace = p.conf.Namespace
	}
	metrics := p.metrics
	circuitBreaker := p.circuitBreaker
	retryManager := p.retryManager
	p.mu.RUnlock()

	if sdk == nil || circuitBreaker == nil || retryManager == nil {
		return nil, NewInitError("Polaris plugin has been destroyed")
	}

	// Record service discovery operation metrics
	if metrics != nil {
		metrics.RecordServiceDiscovery(serviceName, namespace, "start")
		defer func() {
			if metrics != nil {
				metrics.RecordServiceDiscovery(serviceName, namespace, "success")
			}
		}()
	}

	log.Infof("Getting service instances for: %s", serviceName)

	// Execute operation with circuit breaker and retry mechanism
	var instances []model.Instance
	var lastErr error

	// Wrap retry operation with circuit breaker
	err := circuitBreaker.Do(func() error {
		return retryManager.DoWithRetry(func() error {
			// Create Consumer API client
			consumerAPI := api.NewConsumerAPIByContext(sdk)
			if consumerAPI == nil {
				return NewInitError("failed to create consumer API")
			}

			// Build service discovery request
			req := &api.GetInstancesRequest{
				GetInstancesRequest: model.GetInstancesRequest{
					Service:   serviceName,
					Namespace: namespace,
				},
			}

			// Call SDK API to get service instances
			resp, err := consumerAPI.GetInstances(req)
			if err != nil {
				lastErr = err
				return err
			}

			instances = resp.Instances
			return nil
		})
	})

	if err != nil {
		log.Errorf("Failed to get instances for service %s after retries: %v", serviceName, err)
		if metrics != nil {
			metrics.RecordServiceDiscovery(serviceName, namespace, "error")
		}

		return nil, WrapServiceError(lastErr, ErrCodeServiceUnavailable, "failed to get service instances")
	}

	log.Infof("Successfully got %d instances for service %s", len(instances), serviceName)
	return instances, nil
}

// WatchService watches service changes - uses double-checked locking pattern to improve concurrency safety
func (p *PlugPolaris) WatchService(serviceName string) (*ServiceWatcher, error) {
	if err := p.checkInitialized(); err != nil {
		return nil, err
	}

	// Record service watch operation metrics
	if p.metrics != nil {
		p.metrics.RecordSDKOperation("watch_service", "start")
		defer func() {
			if p.metrics != nil {
				p.metrics.RecordSDKOperation("watch_service", "success")
			}
		}()
	}

	log.Infof("Watching service: %s", serviceName)

	// Snapshot mutable plugin state under the lock to avoid a data race / nil
	// dereference if cleanup runs concurrently.
	p.mu.RLock()
	sdk := p.sdk
	namespace := ""
	if p.conf != nil {
		namespace = p.conf.Namespace
	}
	p.mu.RUnlock()

	if sdk == nil {
		return nil, NewInitError("Polaris plugin has been destroyed")
	}

	// First check (read lock)
	p.watcherMutex.RLock()
	if existingWatcher, exists := p.activeWatchers[serviceName]; exists {
		p.watcherMutex.RUnlock()
		log.Infof("Service %s is already being watched", serviceName)
		return existingWatcher, nil
	}
	p.watcherMutex.RUnlock()

	// Create Consumer API client
	consumerAPI := api.NewConsumerAPIByContext(sdk)
	if consumerAPI == nil {
		return nil, NewInitError("failed to create consumer API")
	}

	// Create service watcher and connect to SDK
	watcher := NewServiceWatcherWithContext(p.watcherContext(), consumerAPI, serviceName, namespace)

	// Second check (write lock) - double-checked locking pattern
	p.watcherMutex.Lock()
	defer p.watcherMutex.Unlock()

	// Check again if another goroutine has already created the watcher
	if existingWatcher, exists := p.activeWatchers[serviceName]; exists {
		log.Infof("Service %s watcher was created by another goroutine", serviceName)
		return existingWatcher, nil
	}

	// Register watcher
	p.activeWatchers[serviceName] = watcher

	// Set callback functions
	watcher.SetOnInstancesChanged(func(instances []model.Instance) {
		p.handleServiceInstancesChanged(serviceName, instances)
	})

	watcher.SetOnError(func(err error) {
		p.handleServiceWatchError(serviceName, err)
	})

	// Start watching
	watcher.Start()

	log.Infof("Started watching service: %s", serviceName)
	return watcher, nil
}

// checkServiceHealth checks service health status
func (p *PlugPolaris) checkServiceHealth(serviceName string, instances []model.Instance) {
	healthyCount := 0
	unhealthyCount := 0
	isolatedCount := 0

	for _, instance := range instances {
		if instance == nil {
			continue
		}
		if instance.IsIsolated() {
			isolatedCount++
		} else if instance.IsHealthy() {
			healthyCount++
		} else {
			unhealthyCount++
		}
	}

	// Record health status metrics
	p.mu.RLock()
	metrics := p.metrics
	p.mu.RUnlock()
	if metrics != nil {
		// Record healthy instance count
		log.Infof("Service health metrics: %s - Healthy: %d, Unhealthy: %d, Isolated: %d",
			serviceName, healthyCount, unhealthyCount, isolatedCount)
	}

	// Issue warning if too few healthy instances
	if healthyCount == 0 && len(instances) > 0 {
		log.Warnf("Service %s has no healthy instances! Total: %d, Unhealthy: %d, Isolated: %d",
			serviceName, len(instances), unhealthyCount, isolatedCount)
	} else if healthyCount < len(instances)/2 {
		log.Warnf("Service %s has low healthy instance ratio: %d/%d",
			serviceName, healthyCount, len(instances))
	}
}

// tryStartServiceWatchRetry marks service as retrying and returns true if this goroutine should run the retry.
// Returns false if a retry is already in progress for this service (deduplication).
func (p *PlugPolaris) tryStartServiceWatchRetry(serviceName string) bool {
	p.retryMutex.Lock()
	defer p.retryMutex.Unlock()
	if p.retryingServiceWatchers == nil {
		return false // plugin destroyed or not initialized
	}
	if _, exists := p.retryingServiceWatchers[serviceName]; exists {
		return false
	}
	p.retryingServiceWatchers[serviceName] = struct{}{}
	return true
}

// finishServiceWatchRetry removes service from retrying set (call when retry completes).
func (p *PlugPolaris) finishServiceWatchRetry(serviceName string) {
	p.retryMutex.Lock()
	defer p.retryMutex.Unlock()
	if p.retryingServiceWatchers != nil {
		delete(p.retryingServiceWatchers, serviceName)
	}
}

// retryServiceWatch retries service watch
func (p *PlugPolaris) retryServiceWatch(serviceName string) {
	defer p.finishServiceWatchRetry(serviceName)
	log.Infof("Retrying service watch for %s", serviceName)

	if p.waitForRetryDelay(5 * time.Second) {
		log.Infof("Service watch retry canceled due to plugin shutdown: %s", serviceName)
		return
	}

	if p.IsDestroyed() {
		return
	}

	// Recreate watcher
	if _, err := p.WatchService(serviceName); err == nil {
		log.Infof("Successfully recreated service watcher for %s", serviceName)
	} else {
		log.Errorf("Failed to recreate service watcher for %s: %v", serviceName, err)
	}
}

// useCachedServiceInstances uses cached service instances
func (p *PlugPolaris) useCachedServiceInstances(serviceName string) {
	log.Infof("Using cached service instances for %s", serviceName)
	// Here you can implement logic to get service instances from cache
}

// switchToBackupDiscovery switches to backup service discovery
func (p *PlugPolaris) switchToBackupDiscovery(serviceName string) {
	log.Infof("Switching to backup discovery for %s", serviceName)
	// Here you can implement logic to switch to backup service discovery
}

// notifyDegradationMode logs a degradation-mode activation for the given service.
// Extend this method to integrate with your alerting or event-bus infrastructure.
func (p *PlugPolaris) notifyDegradationMode(serviceName string, info map[string]any) {
	log.Infof("Notifying degradation mode for %s: %+v", serviceName, info)
}
