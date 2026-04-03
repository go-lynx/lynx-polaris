package polaris

import (
	"context"
	"fmt"
	"time"

	kratospolaris "github.com/go-kratos/kratos/contrib/polaris/v2"
	"github.com/go-lynx/lynx/log"
	"github.com/go-lynx/lynx/plugins"
)

func (p *PlugPolaris) PluginProtocol() plugins.PluginProtocol {
	protocol := p.BasePlugin.PluginProtocol()
	protocol.ContextLifecycle = true
	return protocol
}

func (p *PlugPolaris) IsContextAware() bool {
	return true
}

func (p *PlugPolaris) InitializeContext(ctx context.Context, plugin plugins.Plugin, rt plugins.Runtime) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("initialize canceled before start: %w", err)
	}
	return p.BasePlugin.Initialize(plugin, rt)
}

func (p *PlugPolaris) StartContext(ctx context.Context, plugin plugins.Plugin) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start canceled before execution: %w", err)
	}
	if p.Status(plugin) == plugins.StatusActive {
		return plugins.ErrPluginAlreadyActive
	}

	p.SetStatus(plugins.StatusInitializing)
	p.EmitEvent(plugins.PluginEvent{
		Type:     plugins.EventPluginStarting,
		Priority: plugins.PriorityNormal,
		Source:   "StartContext",
		Category: "lifecycle",
	})

	if err := p.startupTasksContext(ctx); err != nil {
		p.SetStatus(plugins.StatusFailed)
		return plugins.NewPluginError(p.ID(), "Start", "Failed to perform startup tasks", err)
	}

	if err := p.checkHealthContext(ctx); err != nil {
		p.SetStatus(plugins.StatusFailed)
		log.Errorf("Plugin %s health check failed: %v", plugin.Name(), err)
		return fmt.Errorf("plugin %s health check failed: %w", plugin.Name(), err)
	}

	p.SetStatus(plugins.StatusActive)
	p.EmitEvent(plugins.PluginEvent{
		Type:     plugins.EventPluginStarted,
		Priority: plugins.PriorityNormal,
		Source:   "StartContext",
		Category: "lifecycle",
	})

	return nil
}

func (p *PlugPolaris) StopContext(ctx context.Context, plugin plugins.Plugin) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stop canceled before execution: %w", err)
	}
	if p.Status(plugin) != plugins.StatusActive {
		return plugins.NewPluginError(p.ID(), "Stop", "Plugin must be active to stop", plugins.ErrPluginNotActive)
	}

	p.SetStatus(plugins.StatusStopping)
	p.EmitEvent(plugins.PluginEvent{
		Type:     plugins.EventPluginStopping,
		Priority: plugins.PriorityNormal,
		Source:   "StopContext",
		Category: "lifecycle",
	})

	if err := p.cleanupTasksContext(ctx); err != nil {
		p.SetStatus(plugins.StatusFailed)
		return plugins.NewPluginError(p.ID(), "Stop", "Failed to perform cleanup tasks", err)
	}

	p.SetStatus(plugins.StatusTerminated)
	p.EmitEvent(plugins.PluginEvent{
		Type:     plugins.EventPluginStopped,
		Priority: plugins.PriorityNormal,
		Source:   "StopContext",
		Category: "lifecycle",
	})

	return nil
}

func (p *PlugPolaris) startupTasksContext(ctx context.Context) (startErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	p.mu.Lock()
	if p.IsInitialized() {
		p.mu.Unlock()
		return NewInitError("Polaris plugin already initialized")
	}
	p.ensureLifecycleContextLocked()
	p.mu.Unlock()

	defer func() {
		if startErr == nil {
			return
		}
		p.rollbackStartupState()
	}()

	if p.metrics != nil {
		p.metrics.RecordSDKOperation("startup", "start")
		defer func() {
			if p.metrics == nil {
				return
			}
			if startErr != nil {
				p.metrics.RecordSDKOperation("startup", "error")
				return
			}
			p.metrics.RecordSDKOperation("startup", "success")
		}()
	}

	log.Infof("Initializing polaris plugin with namespace: %s", p.conf.Namespace)

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("polaris startup canceled before sdk init: %w", err)
	}
	sdk, err := p.loadPolarisConfiguration()
	if err != nil {
		log.Errorf("Failed to initialize Polaris SDK: %v", err)
		return WrapInitError(err, "failed to initialize Polaris SDK")
	}

	pol := kratospolaris.New(
		sdk,
		kratospolaris.WithService(currentLynxName()),
		kratospolaris.WithNamespace(p.conf.Namespace),
	)

	p.mu.Lock()
	p.sdk = sdk
	p.polaris = &pol
	p.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("polaris startup canceled before publishing runtime resources: %w", err)
	}
	if err := p.publishRuntimeResources(); err != nil {
		log.Errorf("Failed to publish Polaris runtime resources: %v", err)
		return WrapInitError(err, "failed to publish runtime resources")
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("polaris startup canceled before setting control plane: %w", err)
	}
	if err := currentLynxApp().SetControlPlane(p); err != nil {
		log.Errorf("Failed to set control plane: %v", err)
		return WrapInitError(err, "failed to set control plane")
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("polaris startup canceled before loading control plane config: %w", err)
	}
	cfg, err := currentLynxApp().InitControlPlaneConfig()
	if err != nil {
		log.Errorf("Failed to init control plane config: %v", err)
		return WrapInitError(err, "failed to init control plane config")
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("polaris startup canceled before loading dependent plugins: %w", err)
	}
	currentLynxApp().GetPluginManager().LoadPlugins(cfg)

	p.setInitialized()
	log.Infof("Polaris plugin initialized successfully")
	return nil
}

func (p *PlugPolaris) ensureLifecycleContextLocked() {
	if p.healthCheckCh == nil {
		p.healthCheckCh = make(chan struct{})
	}
	if p.lifecycleCtx != nil {
		select {
		case <-p.lifecycleCtx.Done():
			p.lifecycleCtx = nil
			p.lifecycleStop = nil
		default:
			return
		}
	}
	p.lifecycleCtx, p.lifecycleStop = context.WithCancel(context.Background())
}

func (p *PlugPolaris) watcherContext() context.Context {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.lifecycleCtx != nil {
		return p.lifecycleCtx
	}
	return context.Background()
}

func (p *PlugPolaris) lifecycleDone() <-chan struct{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.lifecycleCtx != nil {
		return p.lifecycleCtx.Done()
	}
	if p.healthCheckCh != nil {
		return p.healthCheckCh
	}
	return nil
}

func (p *PlugPolaris) waitForRetryDelay(delay time.Duration) bool {
	done := p.lifecycleDone()
	if done == nil {
		time.Sleep(delay)
		return p.IsDestroyed()
	}

	select {
	case <-done:
		return true
	case <-time.After(delay):
		return false
	}
}

func (p *PlugPolaris) rollbackStartupState() {
	p.stopHealthCheck()
	p.cleanupWatchers()
	p.closeSDKConnection()
	p.destroyPolarisInstance()
	p.mu.Lock()
	if p.lifecycleStop != nil {
		p.lifecycleStop()
	}
	p.lifecycleCtx = nil
	p.lifecycleStop = nil
	p.mu.Unlock()
}
