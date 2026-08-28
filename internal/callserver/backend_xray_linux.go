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
	xraycore "github.com/xtls/xray-core/core"
	_ "github.com/xtls/xray-core/main/json"
	_ "github.com/xtls/xray-core/proxy/blackhole"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/transport/internet/headers/noop"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
)

type EmbeddedXrayBackend struct{}

func (EmbeddedXrayBackend) Start(_ context.Context, address string, clients []callxray.Client) (io.Closer, error) {
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
	return instance, nil
}
