package callserver

import (
	"context"
	"fmt"
	"io"
	"net"
	"reflect"
	"sync"
	"time"

	"github.com/gooog1111/orcheroute/internal/calltransport"
	callxray "github.com/gooog1111/orcheroute/internal/calltransport/xray"
)

type Backend interface {
	Start(context.Context, string, []callxray.Client) (io.Closer, error)
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
	cancel     context.CancelFunc
	listener   net.Listener
	backendRun io.Closer
	status     RuntimeStatus
	snapshot   RuntimeSnapshot
	generation uint64
}

func NewRuntime(backend Backend) *Runtime { return &Runtime{backend: backend} }

func (runtime *Runtime) Status() RuntimeStatus {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return runtime.status
}

func (runtime *Runtime) Apply(manager *Manager) error {
	if manager == nil {
		return fmt.Errorf("call_server_manager_required")
	}
	snapshot, err := manager.RuntimeSnapshot()
	if err != nil {
		return err
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if reflect.DeepEqual(runtime.snapshot, snapshot) && runtime.status.Active == snapshot.Enabled {
		return nil
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
	backendRun, err := runtime.backend.Start(ctx, snapshot.BackendAddress, snapshot.Clients)
	if err != nil {
		cancel()
		return fmt.Errorf("call_server_backend_start: %w", err)
	}
	listener, err := calltransport.ListenDTLSProfiles(snapshot.ListenAddress, snapshot.Keys)
	if err != nil {
		cancel()
		_ = backendRun.Close()
		return err
	}
	runtime.generation++
	generation := runtime.generation
	runtime.cancel, runtime.listener, runtime.backendRun, runtime.snapshot = cancel, listener, backendRun, snapshot
	runtime.status = RuntimeStatus{Active: true, ListenAddress: listener.Addr().String(), BackendAddress: snapshot.BackendAddress, Clients: len(snapshot.Clients), StartedAt: time.Now().Unix()}
	go func() {
		err := calltransport.ServeDTLS(ctx, listener, snapshot.BackendAddress, nil)
		runtime.mu.Lock()
		if runtime.generation != generation {
			runtime.mu.Unlock()
			return
		}
		cancel, activeListener, activeBackend := runtime.cancel, runtime.listener, runtime.backendRun
		runtime.cancel, runtime.listener, runtime.backendRun = nil, nil, nil
		runtime.status.Active = false
		if err != nil {
			runtime.status.LastError = err.Error()
		}
		runtime.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if activeListener != nil {
			_ = activeListener.Close()
		}
		if activeBackend != nil {
			_ = activeBackend.Close()
		}
	}()
	return nil
}

func (runtime *Runtime) stopLocked() {
	runtime.generation++
	if runtime.cancel != nil {
		runtime.cancel()
	}
	if runtime.listener != nil {
		_ = runtime.listener.Close()
	}
	if runtime.backendRun != nil {
		_ = runtime.backendRun.Close()
	}
	runtime.cancel, runtime.listener, runtime.backendRun = nil, nil, nil
	runtime.status.Active = false
}
