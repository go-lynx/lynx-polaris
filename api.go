package polaris

import (
	"fmt"

	"github.com/go-kratos/kratos/contrib/polaris/v2"
	"github.com/go-lynx/lynx"
	"github.com/polarismesh/polaris-go/pkg/model"
)

// GetPolarisPlugin returns the Polaris plugin instance.
func GetPolarisPlugin() (*PlugPolaris, error) {
	p := GetPlugin()
	if p == nil {
		return nil, fmt.Errorf("polaris plugin not found")
	}
	return p, nil
}

// GetServiceInstances returns service instances.
func GetServiceInstances(serviceName string) ([]model.Instance, error) {
	p := GetPlugin()
	if p == nil {
		return nil, fmt.Errorf("polaris plugin not found")
	}
	return p.GetServiceInstances(serviceName)
}

// GetConfig fetches configuration by file name and group.
// Global API: retrieve config content by file name and group.
func GetConfig(fileName, group string) (string, error) {
	p := GetPlugin()
	if p == nil {
		return "", fmt.Errorf("polaris plugin not found")
	}
	return p.GetConfigValue(fileName, group)
}

// WatchService watches service changes.
// Global API: watch change events of the specified service.
func WatchService(serviceName string) (*ServiceWatcher, error) {
	p := GetPlugin()
	if p == nil {
		return nil, fmt.Errorf("polaris plugin not found")
	}
	return p.WatchService(serviceName)
}

// WatchConfig watches configuration changes.
// Global API: watch change events of the specified configuration.
func WatchConfig(fileName, group string) (*ConfigWatcher, error) {
	p := GetPlugin()
	if p == nil {
		return nil, fmt.Errorf("polaris plugin not found")
	}
	return p.WatchConfig(fileName, group)
}

// CheckRateLimit checks rate limit status for a service.
// Global API: check the rate limit status of the specified service.
func CheckRateLimit(serviceName string, labels map[string]string) (bool, error) {
	p := GetPlugin()
	if p == nil {
		return false, fmt.Errorf("polaris plugin not found")
	}
	return p.CheckRateLimit(serviceName, labels)
}

// GetMetrics returns plugin metrics.
// Global API: get metrics exposed by the plugin.
func GetMetrics() *Metrics {
	p := GetPlugin()
	if p == nil {
		return nil
	}
	return p.GetMetrics()
}

// IsHealthy checks plugin health status.
// Global API: verify whether the plugin is healthy.
func IsHealthy() error {
	p := GetPlugin()
	if p == nil {
		return fmt.Errorf("polaris plugin not found")
	}
	return p.CheckHealth()
}

// GetPolaris obtains the Polaris instance from the application's plugin manager.
// The instance can be used to interact with Polaris services (service discovery, config management, etc.).
// It returns a *polaris.Polaris pointing to the instance.
func GetPolaris() *polaris.Polaris {
	p := GetPlugin()
	if p == nil {
		return nil
	}
	return p.polaris
}

// GetPlugin obtains the PlugPolaris plugin instance from the application's plugin manager.
// The instance can be used to invoke methods provided by the plugin.
// It returns a *PlugPolaris pointing to the instance.
func GetPlugin() *PlugPolaris {
	if lynx.Lynx() == nil || lynx.Lynx().GetPluginManager() == nil {
		return nil
	}
	pl := lynx.Lynx().GetPluginManager().GetPlugin(pluginName)
	if pl == nil {
		return nil
	}
	return pl.(*PlugPolaris)
}
