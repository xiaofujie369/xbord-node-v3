package singbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cedar2025/xboard-node/internal/config"
	"github.com/cedar2025/xboard-node/internal/kernel"
	"github.com/cedar2025/xboard-node/internal/model"
	"github.com/miekg/dns"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	singJSON "github.com/sagernet/sing/common/json"
)

// Optional operator-supplied routes-only snapshot; never commit live configs.
func TestPanelRouteSnapshot(t *testing.T) {
	path := os.Getenv("XBOARD_TEST_ROUTES_FILE")
	if path == "" {
		t.Skip("no routes-only snapshot supplied")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var routes []model.RouteRule
	if err = json.Unmarshal(data, &routes); err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{"vless", "hysteria"} {
		cfg := buildConfig(config.KernelConfig{}, &model.NodeSpec{Protocol: protocol, Routes: routes}, nil, kernel.TLSCert{})
		// Check routes against the real core without requiring production keys/certificates.
		delete(cfg, "inbounds")
		cfg["log"] = M{"disabled": true}
		data, err = json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		ctx := include.Context(context.Background())
		opts, err := singJSON.UnmarshalExtendedContext[option.Options](ctx, data)
		if err != nil {
			t.Fatal(err)
		}
		core, err := box.New(box.Options{Context: ctx, Options: opts})
		if err != nil {
			t.Fatal(err)
		}
		if err = core.Start(); err != nil {
			core.Close()
			t.Fatal(err)
		}
		core.Close()
	}
}

func TestPanelMatchersCommentsAndInvalidItems(t *testing.T) {
	warnings := 0
	matches, all := panelMatchers([]string{"# 电商 / 应用\n// comment\n; note\n*.Example.COM", "full:exact.test", "keyword:shop", "regexp:^ok[.]", "192.0.2.7/24", "2001:db8::1", "bad/garbage", "regexp:["}, false, func(string) { warnings++ })
	if all || len(matches) != 5 || warnings != 2 {
		t.Fatalf("all=%v groups=%v warnings=%d", all, matches, warnings)
	}
	if !reflect.DeepEqual(matches[0], M{"domain_suffix": []string{"example.com"}}) {
		t.Fatal(matches)
	}
	if !reflect.DeepEqual(matches[4]["ip_cidr"], []string{"192.0.2.0/24", "2001:db8::1/128"}) {
		t.Fatal(matches)
	}
	empty, all := panelMatchers([]string{"# only comment", "invalid/cidr", "geosite-cn", "geoip:cn"}, false, func(string) {})
	if len(empty) != 0 || all {
		t.Fatal("invalid or empty rule became match-all")
	}
}

func TestPanelDNSAddressFormats(t *testing.T) {
	for _, address := range []string{"223.5.5.5", "2400:3200::1", "local", "udp://[2001:db8::1]:5353", "tcp://1.1.1.1", "tls://dns.example.com", "https://dns.example.com/dns-query", "quic://1.1.1.1"} {
		if _, err := panelDNSServer(address, "test", "bootstrap"); err != nil {
			t.Errorf("valid DNS %s: %v", address, err)
		}
	}
	for _, address := range []string{"https://user:secret@dns.example.com", "udp://1.1.1.1:99999", "tcp://1.1.1.1/path", "https://dns.example.com?q=secret", "ftp://1.1.1.1", ""} {
		if _, err := panelDNSServer(address, "test", "bootstrap"); err == nil {
			t.Errorf("accepted invalid DNS: %s", address)
		}
	}
}

func TestPanelPoliciesScopeOrderingAndNoBroadening(t *testing.T) {
	node := &model.NodeSpec{Protocol: "vless", ServerPort: 12345, Routes: []model.RouteRule{
		{Match: []string{"# only"}, Action: "block"},
		{Match: []string{"*"}, Action: "proxy", ActionValue: "missing"},
		{Match: []string{"full:allowed.test"}, Action: "direct"},
		{Match: []string{"domain:test", "192.0.2.0/24"}, Action: "block"},
		{Match: []string{"*"}, Action: "dns", ActionValue: "1.1.1.1"},
		{Match: []string{"domain:example.com"}, Action: "dns", ActionValue: "223.5.5.5,119.29.29.29"},
	}}
	cfg := buildConfig(config.KernelConfig{}, node, nil, kernel.TLSCert{})
	rules := cfg["route"].(M)["rules"].([]M)
	if len(rules) != 8 {
		t.Fatalf("unexpected rules: %v", rules)
	} // scoped resolve + 4 guard rules (resolve, IPv4, IPv6, private) + 3 terminal rules
	if rules[5]["outbound"] != "direct" || rules[6]["action"] != "reject" || rules[7]["action"] != "reject" {
		t.Fatal(rules)
	}
	for _, r := range rules[5:] {
		if !reflect.DeepEqual(r["inbound"], []string{"vless-in"}) {
			t.Fatal(r)
		}
	}
	dr := cfg["dns"].(M)["rules"].([]M)
	if _, ok := dr[0]["domain_suffix"]; !ok {
		t.Fatal("default DNS came before specific DNS")
	}
	if _, ok := dr[1]["domain_suffix"]; ok {
		t.Fatal("wildcard DNS must be scoped default")
	}
	if len(cfg["dns"].(M)["servers"].([]M)) != 4 {
		t.Fatal("all valid resolvers should be imported")
	}
}

func TestPanelRoutesMergedLocalConfig(t *testing.T) {
	path := t.TempDir() + "/local.json"
	local := `{"outbounds":[{"type":"direct","tag":"local-proxy"}],"dns":{"servers":[{"type":"local","tag":"xboard-panel-dns-system"}],"rules":[{"domain_suffix":["test"],"server":"xboard-panel-dns-system"}]},"route":{"rule_set":[{"type":"inline","tag":"local-set","rules":[{"domain":["set.test"]}]}],"rules":[{"domain":["local.test"],"action":"reject"}]}}`
	if err := os.WriteFile(path, []byte(local), 0600); err != nil {
		t.Fatal(err)
	}
	node := &model.NodeSpec{Protocol: "hysteria", Version: 2, Routes: []model.RouteRule{
		{Match: []string{"rule-set:missing", "rule-set:local-set"}, Action: "proxy", ActionValue: "local-proxy"},
		{Match: []string{"*"}, Action: "dns", ActionValue: "1.1.1.1"},
	}}
	cfg := buildConfig(config.KernelConfig{CustomConfig: path}, node, nil, kernel.TLSCert{})
	rules := cfg["route"].(M)["rules"].([]M)
	if len(rules) != 7 || rules[5]["outbound"] != "local-proxy" || !reflect.DeepEqual(rules[5]["rule_set"], []string{"local-set"}) || !reflect.DeepEqual(rules[5]["inbound"], []string{"hysteria-in"}) {
		t.Fatal(rules)
	}
	d := cfg["dns"].(M)
	if d["final"] != "xboard-panel-dns-system-1" {
		t.Fatal("generated tag collided with local DNS")
	}
	dr := d["rules"].([]M)
	if len(dr) != 2 || dr[0]["server"] != "xboard-panel-dns-1-0" {
		t.Fatal("panel DNS not ahead of local rule")
	}
}

// Exercise the embedded kernel with real HTTP and UDP DNS servers. Only this
// fixture removes private-address guards so loopback targets are reachable.
func TestPanelRoutesDNSAndProxyRuntime(t *testing.T) {
	var specific, fallback atomic.Int32
	startDNS := func(counter *atomic.Int32) string {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		server := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
			counter.Add(1)
			m := new(dns.Msg)
			m.SetReply(q)
			for _, question := range q.Question {
				if question.Qtype == dns.TypeA {
					m.Answer = append(m.Answer, &dns.A{Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 1}, A: net.ParseIP("127.0.0.1")})
				}
			}
			_ = w.WriteMsg(m)
		})}
		go server.ActivateAndServe()
		t.Cleanup(func() { server.Shutdown() })
		return "udp://" + pc.LocalAddr().String()
	}
	first, second := startDNS(&specific), startDNS(&fallback)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "direct-hit") }))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			fmt.Fprint(w, "proxy-hit")
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		fmt.Fprint(rw, "HTTP/1.1 200 Connection Established\r\n\r\n")
		rw.Flush()
		if _, err = http.ReadRequest(rw.Reader); err != nil {
			return
		}
		fmt.Fprint(rw, "HTTP/1.1 200 OK\r\nContent-Length: 9\r\nConnection: close\r\n\r\nproxy-hit")
		rw.Flush()
	}))
	defer upstream.Close()
	upstreamURL, _ := url.Parse(upstream.URL)
	_, port, _ := net.SplitHostPort(upstreamURL.Host)
	var upstreamPort int
	fmt.Sscanf(port, "%d", &upstreamPort)
	node := &model.NodeSpec{Protocol: "vless", Routes: []model.RouteRule{
		{Match: []string{"*"}, Action: "dns", ActionValue: second},
		{Match: []string{"domain:specific.test"}, Action: "dns", ActionValue: first},
		{Match: []string{"full:block.specific.test"}, Action: "block"},
		{Match: []string{"full:proxy.specific.test"}, Action: "proxy", ActionValue: "upstream"},
		{Match: []string{"*"}, Action: "direct"},
	}}
	cfg := buildConfig(config.KernelConfig{CustomOutbound: []map[string]any{{"type": "http", "tag": "upstream", "server": "127.0.0.1", "server_port": upstreamPort}}}, node, nil, kernel.TLSCert{})
	guards := ssrfGuardRules()
	var rules []M
	for _, r := range cfg["route"].(M)["rules"].([]M) {
		if !isGuardRule(r, guards) {
			rules = append(rules, r)
		}
	}
	cfg["route"].(M)["rules"] = rules
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listenPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cfg["inbounds"] = []M{{"type": "mixed", "tag": "vless-in", "listen": "127.0.0.1", "listen_port": listenPort}}
	cfg["log"] = M{"disabled": true}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := include.Context(context.Background())
	opts, err := singJSON.UnmarshalExtendedContext[option.Options](ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	core, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	if err = core.Start(); err != nil {
		t.Fatal(err)
	}
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", listenPort))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	_, targetPort, _ := net.SplitHostPort(strings.TrimPrefix(target.URL, "http://"))
	for _, tc := range []struct{ host, want string }{{"ok.specific.test", "direct-hit"}, {"ok.other.test", "direct-hit"}, {"proxy.specific.test", "proxy-hit"}} {
		specificBefore, fallbackBefore := specific.Load(), fallback.Load()
		response, err := client.Get("http://" + tc.host + ":" + targetPort)
		if err != nil {
			t.Fatalf("%s: %v", tc.host, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if string(body) != tc.want {
			t.Fatalf("%s: %q", tc.host, body)
		}
		if strings.HasSuffix(tc.host, ".specific.test") {
			if specific.Load() <= specificBefore || fallback.Load() != fallbackBefore {
				t.Fatalf("specific DNS selection failed for %s", tc.host)
			}
		} else if fallback.Load() <= fallbackBefore || specific.Load() != specificBefore {
			t.Fatalf("default DNS selection failed for %s", tc.host)
		}
	}
	response, err := client.Get("http://block.specific.test:" + targetPort)
	if err == nil {
		response.Body.Close()
		if response.StatusCode < 400 {
			t.Fatal("block policy did not block")
		}
	}
	if specific.Load() == 0 || fallback.Load() == 0 {
		t.Fatalf("DNS policies not used: specific=%d fallback=%d", specific.Load(), fallback.Load())
	}
}
