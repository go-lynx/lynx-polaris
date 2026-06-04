package polaris

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-lynx/lynx/log"
	"github.com/polarismesh/polaris-go/api"
	"github.com/polarismesh/polaris-go/pkg/model"
)

// CheckHealth performs a health check.
func (p *PlugPolaris) CheckHealth() error {
	return p.checkHealthContext(context.Background())
}

func (p *PlugPolaris) checkHealthContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.checkInitialized(); err != nil {
		return err
	}

	// Snapshot mutable plugin state under the lock to avoid a data race / nil
	// dereference if cleanup runs concurrently with this health check.
	p.mu.RLock()
	pol := p.polaris
	sdk := p.sdk
	namespace := ""
	if p.conf != nil {
		namespace = p.conf.Namespace
	}
	p.mu.RUnlock()

	// Check Polaris instance
	if pol == nil {
		return NewInitError("Polaris instance is nil")
	}

	// Check SDK connection
	if sdk == nil {
		return NewInitError("Polaris SDK context is nil")
	}

	// Perform actual health check of the Polaris control plane
	return p.checkPolarisControlPlaneHealthContext(ctx, sdk, namespace)
}

// checkPolarisControlPlaneHealth checks the health of the Polaris control plane.
func (p *PlugPolaris) checkPolarisControlPlaneHealthContext(ctx context.Context, sdk api.SDKContext, namespace string) error {
	// Snapshot metrics/breaker/retry under the lock for the same reason.
	p.mu.RLock()
	metrics := p.metrics
	circuitBreaker := p.circuitBreaker
	retryManager := p.retryManager
	p.mu.RUnlock()

	if circuitBreaker == nil || retryManager == nil {
		return NewInitError("Polaris plugin has been destroyed")
	}

	// Record the start of the health check
	if metrics != nil {
		metrics.RecordHealthCheck("polaris", "start")
		defer func() {
			if metrics != nil {
				metrics.RecordHealthCheck("polaris", "success")
			}
		}()
	}

	log.Infof("Checking Polaris control plane health")

	// Execute health checks using circuit breaker and retry mechanisms
	var healthErr error
	err := circuitBreaker.Do(func() error {
		return retryManager.DoWithRetryContext(ctx, func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			// 1) Check SDK connection status
			if err := p.checkSDKConnection(sdk, namespace); err != nil {
				healthErr = err
				return err
			}

			// 2) Check service discovery functionality
			if err := p.checkServiceDiscoveryHealth(sdk, namespace); err != nil {
				healthErr = err
				return err
			}

			// 3) Check configuration management functionality
			if err := p.checkConfigManagementHealth(sdk, namespace); err != nil {
				healthErr = err
				return err
			}

			// 4) Check rate limiting functionality
			if err := p.checkRateLimitHealth(); err != nil {
				healthErr = err
				return err
			}

			return nil
		})
	})

	if err != nil {
		log.Errorf("Polaris control plane health check failed: %v", healthErr)
		if metrics != nil {
			metrics.RecordHealthCheck("polaris", "error")
		}
		return WrapServiceError(healthErr, ErrCodeServiceUnavailable, "Polaris control plane health check failed")
	}

	log.Infof("Polaris control plane health check passed")
	return nil
}

// checkSDKConnection verifies SDK connection status.
func (p *PlugPolaris) checkSDKConnection(sdk api.SDKContext, namespace string) error {
	// Try to create a Consumer API client to validate connectivity
	consumerAPI := api.NewConsumerAPIByContext(sdk)
	if consumerAPI == nil {
		return fmt.Errorf("failed to create consumer API client")
	}

	// Try to create a simple service discovery request to validate connectivity
	req := &api.GetInstancesRequest{
		GetInstancesRequest: model.GetInstancesRequest{
			Service:   "health-check-service", // use a test service name
			Namespace: namespace,
		},
	}

	// Try calling the API; even if the service does not exist, the connection should succeed
	_, err := consumerAPI.GetInstances(req)
	if err != nil {
		// If the error indicates the service is not found, connectivity is fine
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no instances") {
			log.Debugf("SDK connection test passed (service not found is expected)")
			return nil
		}
		return fmt.Errorf("SDK connection test failed: %v", err)
	}

	return nil
}

// checkServiceDiscoveryHealth checks service discovery with a real GetInstances probe.
func (p *PlugPolaris) checkServiceDiscoveryHealth(sdk api.SDKContext, namespace string) error {
	consumerAPI := api.NewConsumerAPIByContext(sdk)
	if consumerAPI == nil {
		return fmt.Errorf("failed to create consumer API for discovery probe")
	}
	req := &api.GetInstancesRequest{
		GetInstancesRequest: model.GetInstancesRequest{
			Service:   "lynx-polaris-health-probe",
			Namespace: namespace,
		},
	}
	_, err := consumerAPI.GetInstances(req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no instances") {
			log.Debugf("Service discovery probe passed (service not found is expected)")
			return nil
		}
		return fmt.Errorf("service discovery probe failed: %w", err)
	}
	log.Debugf("Service discovery health: probe OK")
	return nil
}

// checkConfigManagementHealth checks configuration management with a real GetConfigFile probe.
func (p *PlugPolaris) checkConfigManagementHealth(sdk api.SDKContext, namespace string) error {
	configAPI := api.NewConfigFileAPIBySDKContext(sdk)
	if configAPI == nil {
		return fmt.Errorf("failed to create config API for config probe")
	}
	_, err := configAPI.GetConfigFile(namespace, "DEFAULT_GROUP", "lynx-polaris-health-probe.yaml")
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "NotFound") {
			log.Debugf("Config management probe passed (file not found is expected)")
			return nil
		}
		return fmt.Errorf("config management probe failed: %w", err)
	}
	log.Debugf("Config management health: probe OK")
	return nil
}

// checkRateLimitHealth checks rate limiting functionality.
func (p *PlugPolaris) checkRateLimitHealth() error {
	// Check status of components related to rate limiting
	if p.circuitBreaker == nil {
		return fmt.Errorf("circuit breaker not initialized")
	}

	if p.retryManager == nil {
		return fmt.Errorf("retry manager not initialized")
	}

	// Check circuit breaker state
	breakerState := p.circuitBreaker.GetState()
	log.Debugf("Rate limit health: circuit breaker state = %d", breakerState)

	return nil
}
