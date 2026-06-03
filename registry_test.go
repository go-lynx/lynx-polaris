package polaris

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// PolarisRegistrar — in-memory store tests (nil provider, testing local logic)
// ---------------------------------------------------------------------------

// newTestRegistrar builds a PolarisRegistrar whose provider is nil.  All
// direct SDK calls (Register/Deregister) will return an error in production,
// but we can still exercise GetService, Watch, and the guard logic.
func newTestRegistrar(namespace string) *PolarisRegistrar {
	return &PolarisRegistrar{
		provider:  nil,
		namespace: namespace,
		instances: make(map[string]*registry.ServiceInstance),
	}
}

func TestPolarisRegistrar_Register_NilService(t *testing.T) {
	reg := newTestRegistrar("default")
	err := reg.Register(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestPolarisRegistrar_Register_CanceledContext(t *testing.T) {
	reg := newTestRegistrar("default")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := &registry.ServiceInstance{Name: "svc", Endpoints: []string{"http://localhost:8080"}}
	err := reg.Register(ctx, svc)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPolarisRegistrar_Deregister_NilService(t *testing.T) {
	reg := newTestRegistrar("default")
	err := reg.Deregister(context.Background(), nil)
	assert.Error(t, err)
}

func TestPolarisRegistrar_Deregister_CanceledContext(t *testing.T) {
	reg := newTestRegistrar("default")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := &registry.ServiceInstance{Name: "svc", Endpoints: []string{"http://localhost:8080"}}
	err := reg.Deregister(ctx, svc)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPolarisRegistrar_GetService_Empty(t *testing.T) {
	reg := newTestRegistrar("default")
	got, err := reg.GetService(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestPolarisRegistrar_GetService_DirectInject(t *testing.T) {
	// Directly inject an instance into the map to test GetService lookup logic.
	reg := newTestRegistrar("default")
	svc := &registry.ServiceInstance{
		ID:        "id-1",
		Name:      "my-svc",
		Version:   "v2",
		Endpoints: []string{"grpc://127.0.0.1:9000"},
		Metadata:  map[string]string{"region": "cn"},
	}
	reg.mu.Lock()
	reg.instances["my-svc:127.0.0.1:9000"] = cloneRegistryServiceInstance(svc)
	reg.mu.Unlock()

	got, err := reg.GetService(context.Background(), "my-svc")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "my-svc", got[0].Name)
	assert.Equal(t, "v2", got[0].Version)
	assert.Equal(t, "cn", got[0].Metadata["region"])

	// Verify GetService returns a defensive copy.
	got[0].Metadata["region"] = "mutated"
	got2, _ := reg.GetService(context.Background(), "my-svc")
	assert.Equal(t, "cn", got2[0].Metadata["region"])
}

func TestPolarisRegistrar_GetService_MultipleMatchAndNonMatch(t *testing.T) {
	reg := newTestRegistrar("default")
	reg.mu.Lock()
	reg.instances["svc-a:h1:8080"] = &registry.ServiceInstance{Name: "svc-a"}
	reg.instances["svc-a:h2:8080"] = &registry.ServiceInstance{Name: "svc-a"}
	reg.instances["svc-b:h1:8080"] = &registry.ServiceInstance{Name: "svc-b"}
	reg.mu.Unlock()

	got, err := reg.GetService(context.Background(), "svc-a")
	require.NoError(t, err)
	assert.Len(t, got, 2)

	got2, err := reg.GetService(context.Background(), "svc-b")
	require.NoError(t, err)
	assert.Len(t, got2, 1)
}

func TestPolarisRegistrar_Watch(t *testing.T) {
	reg := newTestRegistrar("default")
	w, err := reg.Watch(context.Background(), "svc")
	require.NoError(t, err)
	require.NotNil(t, w)
	require.NoError(t, w.Stop())
}

func TestPolarisRegistrar_ConcurrentGetService(t *testing.T) {
	reg := newTestRegistrar("default")
	reg.mu.Lock()
	reg.instances["svc:h:8080"] = &registry.ServiceInstance{Name: "svc"}
	reg.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = reg.GetService(context.Background(), "svc")
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// PolarisDiscovery — default-config path
// ---------------------------------------------------------------------------

func TestNewPolarisDiscovery_DefaultConfig(t *testing.T) {
	// Pass nil consumer and nil config — should use safe defaults.
	disc := NewPolarisDiscovery(nil, "ns", nil)
	assert.Equal(t, "ns", disc.namespace)
	assert.Equal(t, 5*time.Second, disc.watchInterval)
	assert.Equal(t, true, disc.enableRetry)
	assert.Equal(t, 3, disc.maxRetryTimes)
	assert.Equal(t, 2*time.Second, disc.baseRetry)
}

func TestNewPolarisDiscovery_NilConsumerGetService(t *testing.T) {
	disc := NewPolarisDiscovery(nil, "default", nil)
	_, err := disc.GetService(context.Background(), "svc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestNewPolarisDiscovery_NilConsumerWatch(t *testing.T) {
	disc := NewPolarisDiscovery(nil, "default", nil)
	_, err := disc.Watch(context.Background(), "svc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

// ---------------------------------------------------------------------------
// PolarisWatcher — lifecycle
// ---------------------------------------------------------------------------

func TestPolarisWatcher_Stop_NoCancel(t *testing.T) {
	// A watcher with cancel=nil must not panic on Stop().
	w := &PolarisWatcher{name: "svc"}
	assert.NoError(t, w.Stop())
}

func TestPolarisWatcher_Stop_Idempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := &PolarisWatcher{ctx: ctx, cancel: cancel, name: "svc"}
	assert.NoError(t, w.Stop())
	assert.NoError(t, w.Stop()) // second call must not panic
}

func TestPolarisWatcher_ContextCancel_UnblocksNext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := &PolarisWatcher{
		ctx:          ctx,
		cancel:       cancel,
		name:         "svc",
		consumer:     nil, // nil consumer → Next loops immediately then respects ctx
		pollInterval: 10 * time.Millisecond,
	}

	done := make(chan error, 1)
	go func() {
		_, err := w.Next()
		done <- err
	}()

	// Let the first tick fire (consumer nil → returns empty slice, no block) or
	// cancel and assert that Next unblocks.
	cancel()

	select {
	case err := <-done:
		// Either nil (nil consumer path returns immediately) or ctx error.
		_ = err
	case <-time.After(3 * time.Second):
		t.Fatal("Next() did not unblock after context cancel")
	}
}

// ---------------------------------------------------------------------------
// Health-check guard tests
// ---------------------------------------------------------------------------

func TestCheckHealth_NotInitialized(t *testing.T) {
	plugin := NewPolarisControlPlane()
	err := plugin.CheckHealth()
	assert.Error(t, err)
	var pe *PolarisError
	assert.True(t, errors.As(err, &pe))
}

func TestCheckHealthContext_CanceledContext(t *testing.T) {
	plugin := NewPolarisControlPlane()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := plugin.checkHealthContext(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

// ---------------------------------------------------------------------------
// Plugin-level guard: NewServiceRegistry / NewServiceDiscovery
// ---------------------------------------------------------------------------

func TestPluginRegistrar_Uninitialized(t *testing.T) {
	plugin := NewPolarisControlPlane()
	assert.Nil(t, plugin.NewServiceRegistry())
	assert.Nil(t, plugin.NewServiceDiscovery())
}

// ---------------------------------------------------------------------------
// parseEndpoints — exhaustive edge cases
// ---------------------------------------------------------------------------

func TestParseEndpoints_EmptySlice(t *testing.T) {
	host, port, protocol := parseEndpoints(nil)
	assert.Equal(t, "localhost", host)
	assert.Equal(t, 8080, port)
	assert.Equal(t, "http", protocol)
}

func TestParseEndpoints_EmptyString(t *testing.T) {
	host, port, protocol := parseEndpoints([]string{""})
	assert.Equal(t, "localhost", host)
	assert.Equal(t, 8080, port)
	assert.Equal(t, "http", protocol)
}

func TestParseEndpoints_OnlyScheme(t *testing.T) {
	host, port, protocol := parseEndpoints([]string{"grpc://myservice.local:9090"})
	assert.Equal(t, "myservice.local", host)
	assert.Equal(t, 9090, port)
	assert.Equal(t, "grpc", protocol)
}

func TestParseEndpoints_IPv6(t *testing.T) {
	host, port, protocol := parseEndpoints([]string{"http://[::1]:8082"})
	assert.Equal(t, "::1", host)
	assert.Equal(t, 8082, port)
	assert.Equal(t, "http", protocol)
}

func TestParseEndpoints_HostOnly(t *testing.T) {
	host, port, protocol := parseEndpoints([]string{"service.local"})
	assert.Equal(t, "service.local", host)
	assert.Equal(t, 8080, port) // default port
	assert.Equal(t, "http", protocol)
}

// ---------------------------------------------------------------------------
// cloneRegistryServiceInstance
// ---------------------------------------------------------------------------

func TestCloneRegistryServiceInstance_Nil(t *testing.T) {
	assert.Nil(t, cloneRegistryServiceInstance(nil))
}

func TestCloneRegistryServiceInstance_DefensiveCopy(t *testing.T) {
	original := &registry.ServiceInstance{
		ID:        "id",
		Name:      "svc",
		Endpoints: []string{"http://localhost:8080"},
		Metadata:  map[string]string{"env": "prod"},
	}
	clone := cloneRegistryServiceInstance(original)
	original.Endpoints[0] = "http://mutated:8080"
	original.Metadata["env"] = "mutated"
	assert.Equal(t, "http://localhost:8080", clone.Endpoints[0])
	assert.Equal(t, "prod", clone.Metadata["env"])
}
