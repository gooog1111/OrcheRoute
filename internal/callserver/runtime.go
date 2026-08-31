package callserver

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"
)

type Backend interface {
	Start(context.Context, RuntimeSnapshot) (io.Closer, error)
}

type Traffic struct {
	RXBytes uint64
	TXBytes uint64
}

type trafficReporter interface {
	DrainTraffic() map[string]Traffic
}

type healthReporter interface{ Alive() error }

type relayStarter interface {
	Start(context.Context, RuntimeSnapshot) (io.Closer, error)
}

type RuntimeStatus struct {
	Active         bool   `json:"active"`
	ListenAddress  string `json:"listen_address,omitempty"`
	BackendAddress string `json:"backend_address,omitempty"`
	Clients        int    `json:"clients"`
	StartedAt      int64  `json:"started_at,omitempty"`
	LastError      string `json:"last_error,omitempty"`
}

type Runtime struct {
	mu         sync.Mutex
	backend    Backend
	freeTURN   relayStarter
	cancel     context.CancelFunc
	relayRun   io.Closer
	backendRun io.Closer
	status     RuntimeStatus
	snapshot   RuntimeSnapshot
	generation uint64
}

func NewFreeTURNRuntime(backend Backend, relay FreeTURNRelay) *Runtime {
	return newRuntimeWithRelay(backend, relay)
}

func newRuntimeWithRelay(backend Backend, relay relayStarter) *Runtime {
	return &Runtime{backend: backend, freeTURN: relay}
}

func (runtime *Runtime) Status() RuntimeStatus {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.status
}

func (runtime *Runtime) Apply(manager *Manager) error {
	if manager == nil {
		return fmt.Errorf("call_server_manager_required")
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if err := runtime.syncTrafficLocked(manager); err != nil {
		return err
	}
	snapshot, err := manager.RuntimeSnapshot()
	if err != nil {
		return err
	}
	if reflect.DeepEqual(runtime.snapshot, snapshot) && runtime.status.Active == snapshot.Enabled {
		if !snapshot.Enabled || runtime.healthyLocked() {
			return nil
		}
	}
	runtime.stopLocked()
	if !snapshot.Enabled {
		runtime.snapshot = snapshot
		runtime.status = RuntimeStatus{}
		return nil
	}
	if err := runtime.startLocked(snapshot); err != nil {
		runtime.status.LastError = err.Error()
		return err
	}
	return nil
}

func (runtime *Runtime) healthyLocked() bool {
	for _, running := range []io.Closer{runtime.backendRun, runtime.relayRun} {
		if reporter, ok := running.(healthReporter); ok && reporter.Alive() != nil {
			return false
		}
	}
	return true
}

func (runtime *Runtime) syncTrafficLocked(manager *Manager) error {
	reporter, ok := runtime.backendRun.(trafficReporter)
	if !ok {
		return nil
	}
	known := make(map[string]bool)
	for _, client := range manager.PublicConfig().Clients {
		known[client.ID] = true
	}
	now := time.Now().Unix()
	for id, traffic := range reporter.DrainTraffic() {
		if !known[id] || (traffic.RXBytes == 0 && traffic.TXBytes == 0) {
			continue
		}
		if err := manager.RecordTraffic(context.Background(), id, traffic.RXBytes, traffic.TXBytes, now); err != nil {
			return err
		}
	}
	return nil
}

func (runtime *Runtime) Close() error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.stopLocked()
	runtime.snapshot = RuntimeSnapshot{}
	runtime.status = RuntimeStatus{}
	return nil
}

func (runtime *Runtime) startLocked(snapshot RuntimeSnapshot) error {
	if runtime.backend == nil {
		return fmt.Errorf("call_server_backend_unavailable")
	}
	if len(snapshot.Keys) == 0 || len(snapshot.Clients) == 0 {
		return fmt.Errorf("call_server_no_active_clients")
	}
	ctx, cancel := context.WithCancel(context.Background())
	backendRun, err := runtime.backend.Start(ctx, snapshot)
	if err != nil {
		cancel()
		return fmt.Errorf("call_server_backend_start: %w", err)
	}
	if runtime.freeTURN == nil {
		cancel()
		_ = backendRun.Close()
		return fmt.Errorf("call_server_freeturn_unavailable")
	}
	relayRun, relayErr := runtime.freeTURN.Start(ctx, snapshot)
	if relayErr != nil {
		cancel()
		_ = backendRun.Close()
		return fmt.Errorf("call_server_freeturn_start: %w", relayErr)
	}
	runtime.generation++
	runtime.cancel, runtime.relayRun, runtime.backendRun, runtime.snapshot = cancel, relayRun, backendRun, snapshot
	runtime.status = RuntimeStatus{Active: true, ListenAddress: snapshot.ListenAddress, BackendAddress: snapshot.BackendAddress, Clients: len(snapshot.Clients), StartedAt: time.Now().Unix()}
	return nil
}

func (runtime *Runtime) stopLocked() {
	runtime.generation++
	if runtime.cancel != nil {
		runtime.cancel()
	}
	if runtime.relayRun != nil {
		_ = runtime.relayRun.Close()
	}
	if runtime.backendRun != nil {
		_ = runtime.backendRun.Close()
	}
	runtime.cancel, runtime.relayRun, runtime.backendRun = nil, nil, nil
	runtime.status.Active = false
}
