//go:build linux

package callserver

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	callxray "github.com/gooog1111/orcheroute/internal/calltransport/xray"
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/log"
	_ "github.com/xtls/xray-core/app/policy"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/app/stats"
	xraycore "github.com/xtls/xray-core/core"
	xraystats "github.com/xtls/xray-core/features/stats"
	_ "github.com/xtls/xray-core/main/json"
	_ "github.com/xtls/xray-core/proxy/blackhole"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/transport/internet/headers/noop"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
)

type EmbeddedXrayBackend struct {
	MihomoBinary   string
	StateDirectory string
}

type embeddedXrayRuntime struct {
	instance *xraycore.Instance
	stats    xraystats.Manager
	clients  []string
	ordinary io.Closer
}

func (runtime *embeddedXrayRuntime) Close() error {
	if runtime.ordinary != nil {
		_ = runtime.ordinary.Close()
	}
	return runtime.instance.Close()
}

func (runtime *embeddedXrayRuntime) Alive() error {
	if runtime.ordinary == nil {
		return nil
	}
	if reporter, ok := runtime.ordinary.(healthReporter); ok {
		return reporter.Alive()
	}
	return nil
}

func (runtime *embeddedXrayRuntime) DrainTraffic() map[string]Traffic {
	result := make(map[string]Traffic, len(runtime.clients))
	for _, id := range runtime.clients {
		uplink := runtime.stats.GetCounter("user>>>" + id + ">>>traffic>>>uplink")
		downlink := runtime.stats.GetCounter("user>>>" + id + ">>>traffic>>>downlink")
		traffic := Traffic{}
		if uplink != nil {
			traffic.RXBytes = positiveBytes(uplink.Set(0))
		}
		if downlink != nil {
			traffic.TXBytes = positiveBytes(downlink.Set(0))
		}
		if traffic.RXBytes != 0 || traffic.TXBytes != 0 {
			result[id] = traffic
		}
	}
	return result
}

func positiveBytes(value int64) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func (backend EmbeddedXrayBackend) Start(ctx context.Context, snapshot RuntimeSnapshot) (io.Closer, error) {
	address, clients := snapshot.BackendAddress, snapshot.Clients
	host, portValue, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("call_server_invalid_backend_address")
	}
	port, err := strconv.Atoi(portValue)
	if err != nil {
		return nil, fmt.Errorf("call_server_invalid_backend_address")
	}
	config, err := callxray.ServerConfig(callxray.ServerInput{ListenAddress: host, ListenPort: port, Clients: clients})
	if err != nil {
		return nil, err
	}
	instance, err := xraycore.StartInstance("json", config)
	if err != nil {
		return nil, fmt.Errorf("call_server_xray_start: %w", err)
	}
	manager, ok := instance.GetFeature(xraystats.ManagerType()).(xraystats.Manager)
	if !ok {
		_ = instance.Close()
		return nil, fmt.Errorf("call_server_xray_stats_unavailable")
	}
	ids := make([]string, 0, len(clients))
	for _, client := range clients {
		ids = append(ids, client.Email)
	}
	running := &embeddedXrayRuntime{instance: instance, stats: manager, clients: ids}
	if snapshot.Ordinary.Enabled {
		if backend.MihomoBinary == "" || backend.StateDirectory == "" {
			_ = running.Close()
			return nil, fmt.Errorf("call_server_mihomo_unavailable")
		}
		ordinary, err := (ordinaryMihomoBackend{Binary: backend.MihomoBinary, StateDirectory: backend.StateDirectory}).Start(ctx, snapshot.Ordinary)
		if err != nil {
			_ = running.Close()
			return nil, err
		}
		running.ordinary = ordinary
	}
	return running, nil
}
