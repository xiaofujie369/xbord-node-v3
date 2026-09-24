//go:build with_utls

package singbox

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cedar2025/xboard-node/internal/config"
	"github.com/cedar2025/xboard-node/internal/kernel"
	"github.com/cedar2025/xboard-node/internal/model"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	singJSON "github.com/sagernet/sing/common/json"
)

func anyTLSFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// Uses the project's embedded runtime and a real native AnyTLS client. All
// listeners, generated keys and the REALITY handshake target are local.
func TestAnyTLSRealityLocalRoundTrip(t *testing.T) {
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "handshake target") }))
	target.TLS = &tls.Config{MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}}
	target.StartTLS()
	defer target.Close()
	destination := strings.TrimPrefix(target.URL, "https://")
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	node := &model.NodeSpec{Protocol: "anytls", ListenIP: "127.0.0.1", ServerPort: anyTLSFreePort(t), TLS: 2, PaddingScheme: "stop=8\n0=30-30", TLSSettings: map[string]any{
		"private_key": base64.RawURLEncoding.EncodeToString(key.Bytes()), "short_id": "0123456789abcdef", "dest": destination, "server_name": "example.com"}}
	users := []model.UserSpec{{ID: 1, UUID: "local-integration-user"}}
	server := New(config.KernelConfig{Type: "singbox"})
	if err := server.Start(node, users, kernel.TLSCert{}); err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(w, r.Body) }))
	defer echo.Close()

	request := func(password string) error {
		port := anyTLSFreePort(t)
		clientConfig := M{"log": M{"disabled": true}, "inbounds": []M{{"type": "mixed", "tag": "local-client", "listen": "127.0.0.1", "listen_port": port}}, "outbounds": []M{{"type": "anytls", "tag": "proxy", "server": "127.0.0.1", "server_port": node.ServerPort, "password": password, "tls": M{"enabled": true, "server_name": "example.com", "utls": M{"enabled": true, "fingerprint": "chrome"}, "reality": M{"enabled": true, "public_key": base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), "short_id": "0123456789abcdef"}}}}}
		data, _ := json.Marshal(clientConfig)
		ctx, cancel := context.WithCancel(include.Context(context.Background()))
		defer cancel()
		opts, err := singJSON.UnmarshalExtendedContext[option.Options](ctx, data)
		if err != nil {
			return err
		}
		client, err := box.New(box.Options{Context: ctx, Options: opts})
		if err != nil {
			return err
		}
		defer client.Close()
		if err = client.Start(); err != nil {
			return err
		}
		proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		payload := strings.Repeat("anytls-reality-round-trip\n", 1024)
		response, err := httpClient.Post(echo.URL, "text/plain", strings.NewReader(payload))
		if err != nil {
			return err
		}
		defer response.Body.Close()
		result, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		if response.StatusCode != 200 || string(result) != payload {
			return fmt.Errorf("round trip failed: status=%d bytes=%d", response.StatusCode, len(result))
		}
		return nil
	}
	if err := request(users[0].UUID); err != nil {
		t.Fatal(err)
	}
	traffic, alive, _, err := server.GetUserTraffic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if traffic[1][0] == 0 || traffic[1][1] == 0 {
		t.Fatalf("missing per-user traffic: %v", traffic)
	}
	if !alive[1]["127.0.0.1"] {
		t.Fatalf("missing online IP: %v", alive)
	}
	if _, _, err := server.UpdateUsers([]model.UserSpec{{ID: 2, UUID: "replacement-local-user"}}); err != nil {
		t.Fatal(err)
	}
	if err := request("replacement-local-user"); err != nil {
		t.Fatal(err)
	}
	if err := request(users[0].UUID); err == nil {
		t.Fatal("removed user still authenticates")
	}
	// Rebuild the inbound on the same port with new per-node settings.
	updated := *node
	updated.TLSSettings = map[string]any{"private_key": node.TLSSettings["private_key"], "short_id": []string{"0123456789abcdef", "1111111111111111"}, "dest": destination, "server_name": "example.com"}
	if err := server.Reload(&updated, []model.UserSpec{{ID: 2, UUID: "replacement-local-user"}}, kernel.TLSCert{}); err != nil {
		t.Fatal(err)
	}
	if err := request("replacement-local-user"); err != nil {
		t.Fatal(err)
	}
}

func TestAnyTLSNoTLSRuntime(t *testing.T) {
	server := New(config.KernelConfig{Type: "singbox"})
	if err := server.Start(&model.NodeSpec{Protocol: "anytls", ListenIP: "127.0.0.1", ServerPort: anyTLSFreePort(t)}, []model.UserSpec{{ID: 1, UUID: "local-user"}}, kernel.TLSCert{}); err != nil {
		t.Fatal(err)
	}
	server.Stop()
}
