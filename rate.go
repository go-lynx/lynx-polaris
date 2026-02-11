package polaris

import (
	"github.com/go-kratos/kratos/contrib/polaris/v2"
	"github.com/go-kratos/kratos/v2/selector"
	"github.com/go-lynx/lynx/log"
)

// NewNodeRouter creates Polaris node filter
// Used for synchronizing remote service routing policies
func (p *PlugPolaris) NewNodeRouter(name string) selector.NodeFilter {
	if err := p.checkInitialized(); err != nil {
		log.Warnf("Polaris plugin not initialized, returning nil node router: %v", err)
		return nil
	}
	if p.polaris == nil {
		log.Warnf("Polaris instance is nil, returning nil node router")
		return nil
	}
	log.Infof("Synchronizing [%v] routing policy", name)
	return p.polaris.NodeFilter(polaris.WithRouterService(name))
}
