package polaris

import (
	"fmt"
	"time"

	"github.com/go-lynx/lynx/log"
	"github.com/polarismesh/polaris-go/pkg/model"
)

// updateServiceInstanceCache updates the in-memory service-instance cache for the given service.
func (p *PlugPolaris) updateServiceInstanceCache(serviceName string, instances []model.Instance) {
	if p.conf == nil {
		return
	}
	cacheKey := fmt.Sprintf("service:%s:%s", p.conf.Namespace, serviceName)

	cacheData := map[string]any{
		"service_name": serviceName,
		"namespace":    p.conf.Namespace,
		"instances":    instances,
		"updated_at":   time.Now().Unix(),
		"count":        len(instances),
	}

	p.cacheMutex.Lock()
	defer p.cacheMutex.Unlock()

	if p.serviceCache == nil {
		p.serviceCache = make(map[string]any)
	}
	p.serviceCache[cacheKey] = cacheData

	log.Infof("Updated service instance cache for %s: %d instances (cache size: %d)",
		serviceName, len(instances), len(p.serviceCache))
}

// updateConfigCache updates the in-memory configuration cache for the given file/group.
func (p *PlugPolaris) updateConfigCache(fileName, group string, config model.ConfigFile) {
	if p.conf == nil || config == nil {
		return
	}
	cacheKey := fmt.Sprintf("config:%s:%s:%s", p.conf.Namespace, group, fileName)

	cacheData := map[string]any{
		"config_file":    fileName,
		"group":          group,
		"namespace":      p.conf.Namespace,
		"content":        config.GetContent(),
		"updated_at":     time.Now().Unix(),
		"content_length": len(config.GetContent()),
	}

	p.cacheMutex.Lock()
	defer p.cacheMutex.Unlock()

	if p.configCache == nil {
		p.configCache = make(map[string]any)
	}
	p.configCache[cacheKey] = cacheData

	log.Infof("Updated config cache for %s:%s, content length: %d (cache size: %d)",
		fileName, group, len(config.GetContent()), len(p.configCache))
}

// clearServiceCache evicts all service-instance cache entries.
func (p *PlugPolaris) clearServiceCache() {
	p.cacheMutex.Lock()
	defer p.cacheMutex.Unlock()

	if p.serviceCache != nil {
		clearedCount := len(p.serviceCache)
		p.serviceCache = make(map[string]any)
		log.Infof("Cleared %d service cache entries", clearedCount)
	}
}

// clearConfigCache evicts all configuration cache entries.
func (p *PlugPolaris) clearConfigCache() {
	p.cacheMutex.Lock()
	defer p.cacheMutex.Unlock()

	if p.configCache != nil {
		clearedCount := len(p.configCache)
		p.configCache = make(map[string]any)
		log.Infof("Cleared %d config cache entries", clearedCount)
	}
}

// getCacheStats returns a snapshot of current cache sizes.
func (p *PlugPolaris) getCacheStats() map[string]any {
	p.cacheMutex.RLock()
	defer p.cacheMutex.RUnlock()

	stats := map[string]any{
		"service_cache_size": 0,
		"config_cache_size":  0,
		"timestamp":          time.Now().Unix(),
	}

	if p.serviceCache != nil {
		stats["service_cache_size"] = len(p.serviceCache)
	}
	if p.configCache != nil {
		stats["config_cache_size"] = len(p.configCache)
	}

	return stats
}
