package xray

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/cedar2025/xboard-node/internal/config"
	"github.com/cedar2025/xboard-node/internal/kernel"
	"github.com/cedar2025/xboard-node/internal/model"
	"github.com/xtls/xray-core/features/inbound"
)

func xrayFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}

// A restart (also used as the reload fallback) must succeed on the same port
// and the node must keep listening afterwards.
func TestXrayRestartOnSamePort(t *testing.T) {
	users := []model.UserSpec{{ID: 1, UUID: "b831381d-6324-4d53-ad4f-8cda48b30811"}}
	node := &model.NodeSpec{Protocol: "vmess", ServerPort: xrayFreePort(t), Network: "tcp"}
	x := New(config.KernelConfig{Type: "xray", LogLevel: "error"})
	if err := x.Start(node, users, kernel.TLSCert{}); err != nil {
		t.Fatal(err)
	}
	defer x.Stop()
	changed := *node
	changed.Network = "tcp"
	users2 := append(users, model.UserSpec{ID: 2, UUID: "c831381d-6324-4d53-ad4f-8cda48b30812"})
	if err := x.Start(&changed, users2, kernel.TLSCert{}); err != nil {
		t.Fatalf("restart on the same port failed: %v", err)
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", node.ServerPort), time.Second)
	if err != nil {
		t.Fatalf("not listening after restart: %v", err)
	}
	conn.Close()
}

// Regression: with SO_REUSEPORT the old instance kept its listener during the
// drain window, so new connections were split between old and new config.
func TestXrayRestartClosesOldListener(t *testing.T) {
	users := []model.UserSpec{{ID: 1, UUID: "b831381d-6324-4d53-ad4f-8cda48b30811"}}
	node := &model.NodeSpec{Protocol: "vmess", ServerPort: xrayFreePort(t), Network: "tcp"}
	x := New(config.KernelConfig{Type: "xray", LogLevel: "error"})
	if err := x.Start(node, users, kernel.TLSCert{}); err != nil {
		t.Fatal(err)
	}
	defer x.Stop()
	x.mu.Lock()
	old := x.instance
	x.mu.Unlock()
	if err := x.Start(node, users, kernel.TLSCert{}); err != nil {
		t.Fatal(err)
	}
	m := old.GetFeature(inbound.ManagerType()).(inbound.Manager)
	for _, h := range m.ListHandlers(context.Background()) {
		if h.Tag() != "" {
			t.Fatalf("old instance still has inbound %q listening after restart", h.Tag())
		}
	}
}
