package singbox

import (
	"github.com/cedar2025/xboard-node/internal/kernel"
	"github.com/cedar2025/xboard-node/internal/panel"
	"testing"
)

func TestMieruTransportNormalization(t *testing.T) {
	for _, input := range []string{"", "tcp", " TCP "} {
		nc := testNodeSpec(&panel.NodeConfig{Protocol: "mieru", ServerPort: 20001, Transport: input, TrafficPattern: "encoded-pattern"})
		inbound := buildInbound(nc, testUsers, kernel.TLSCert{})
		assertMapValue(t, inbound, "transport", "TCP")
		assertMapValue(t, inbound, "traffic_pattern", "encoded-pattern")
		assertMapValue(t, inbound, "listen_port", 20001)
	}
	nc := testNodeSpec(&panel.NodeConfig{Protocol: "mieru", ServerPort: 20001, Transport: "udp"})
	assertMapValue(t, buildInbound(nc, testUsers, kernel.TLSCert{}), "transport", "UDP")
}
