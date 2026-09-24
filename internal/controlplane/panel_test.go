package controlplane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cedar2025/xboard-node/internal/config"
	"github.com/cedar2025/xboard-node/internal/model"
	panelapi "github.com/cedar2025/xboard-node/internal/panel"
)

func TestPanelControlPlaneInitialRejectsInvalidCustomOutbounds(t *testing.T) {
	server := newPanelTestServer(`{"protocol":"shadowsocks","server_port":8388,"custom_outbounds":[{"tag":"proxy","protocol":"socks","proxy_tag":"missing","settings":{"server":"2.2.2.2","server_port":1080}}]}`)
	defer server.Close()

	cp := NewPanelControlPlane(config.PanelConfig{URL: server.URL, Token: "token", NodeID: 1}, config.WSConfig{}, config.KernelConfig{Type: "singbox"})
	_, err := cp.Initial(context.Background(), nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `initial config normalize: validate custom outbounds: custom_outbounds[0].proxy_tag references unknown outbound "missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPanelControlPlanePollRejectsInvalidCustomOutbounds(t *testing.T) {
	server := newPanelTestServer(`{"protocol":"shadowsocks","server_port":8388,"custom_outbounds":[{"tag":"proxy","protocol":"socks","proxy_tag":"missing","settings":{"server":"2.2.2.2","server_port":1080}}]}`)
	defer server.Close()

	cp := NewPanelControlPlane(config.PanelConfig{URL: server.URL, Token: "token", NodeID: 1}, config.WSConfig{}, config.KernelConfig{Type: "singbox"})
	_, err := cp.Poll(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `poll config normalize: validate custom outbounds: custom_outbounds[0].proxy_tag references unknown outbound "missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTranslateWSEventRejectsInvalidCustomOutbounds(t *testing.T) {
	_, err := TranslateWSEvent(panelapi.WSEvent{
		Type: panelapi.WSEventSyncConfig,
		Config: &panelapi.NodeConfig{
			Protocol:   "shadowsocks",
			ServerPort: 8388,
			CustomOutbounds: []panelapi.OutboundConfig{
				{Tag: "proxy", Protocol: "socks", ProxyTag: "missing", Settings: map[string]any{"server": "2.2.2.2", "server_port": 1080}},
			},
		},
	}, config.KernelConfig{Type: "singbox"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `translate node config: validate custom outbounds: custom_outbounds[0].proxy_tag references unknown outbound "missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func newPanelTestServer(configBody string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/server/handshake", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"websocket":{"enabled":false},"settings":{"push_interval":60,"pull_interval":60}}`))
	})
	mux.HandleFunc("/api/v1/server/UniProxy/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(configBody))
	})
	mux.HandleFunc("/api/v1/server/UniProxy/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"users":[{"id":1,"uuid":"11111111-1111-1111-1111-111111111111"}]}`))
	})
	return httptest.NewServer(mux)
}

func TestTranslateWSEventRejectsUnsupportedProtocolForKernel(t *testing.T) {
	_, err := TranslateWSEvent(panelapi.WSEvent{
		Type: panelapi.WSEventSyncConfig,
		Config: &panelapi.NodeConfig{
			Protocol:   "shadowsocks",
			ServerPort: 8388,
			CustomOutbounds: []panelapi.OutboundConfig{
				{Tag: "hy2", Protocol: "hysteria2", Settings: map[string]any{"server": "2.2.2.2", "server_port": 8443}},
			},
		},
	}, config.KernelConfig{Type: "xray"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `translate node config: validate custom outbounds: custom_outbounds[0].protocol "hysteria2" is not supported by kernel "xray"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPanelControlPlaneAnyTLSRealityMapping(t *testing.T) {
	server := newPanelTestServer(`{"protocol":"anytls","server_port":443,"tls":2,"tls_settings":{"private_key":"test-key","dest":"example.com:443","server_name":"example.com","short_id":["11","2222"]},"padding_scheme":["stop=8","0=30-30"]}`)
	defer server.Close()
	cp := NewPanelControlPlane(config.PanelConfig{URL: server.URL, Token: "token", NodeID: 1}, config.WSConfig{}, config.KernelConfig{Type: "singbox"})
	initial, err := cp.Initial(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	polled, err := cp.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []*model.NodeSpec{initial.Config, polled.Config} {
		if spec.TLS != 2 || spec.TLSSettings["private_key"] != "test-key" || spec.PaddingScheme != "stop=8\n0=30-30" {
			t.Fatal("REST mapping lost AnyTLS settings")
		}
		ids, ok := spec.TLSSettings["short_id"].([]any)
		if !ok || len(ids) != 2 {
			t.Fatal("REST lost short ID array")
		}
	}
	node := &panelapi.NodeConfig{Protocol: "anytls", TLS: 2, TLSSettings: map[string]any{"private_key": "test-key", "short_id": "11", "dest": "example.com:443"}}
	event, err := TranslateWSEvent(panelapi.WSEvent{Type: panelapi.WSEventSyncConfig, Config: node}, config.KernelConfig{Type: "singbox"})
	if err != nil {
		t.Fatal(err)
	}
	if event.Config.TLS != 2 || event.Config.TLSSettings["short_id"] != "11" {
		t.Fatal("WS mapping lost REALITY")
	}
	node.TLSSettings["private_key"] = "other-node-key"
	if event.Config.TLSSettings["private_key"] != "test-key" {
		t.Fatal("node settings aliased")
	}
}
