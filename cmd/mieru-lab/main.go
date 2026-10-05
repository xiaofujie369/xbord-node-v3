// mieru-lab generates native traffic patterns and performs bounded, in-process
// proxy checks. It does not open a local proxy listener or change system settings.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"time"

	mclient "github.com/enfein/mieru/v3/apis/client"
	tp "github.com/enfein/mieru/v3/apis/trafficpattern"
	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type targetAddr string

func (a targetAddr) Network() string { return "tcp" }
func (a targetAddr) String() string  { return string(a) }

func pattern(fragment bool) *pb.TrafficPattern {
	return &pb.TrafficPattern{
		Seed: proto.Int32(20261005), UnlockAll: proto.Bool(false),
		TcpFragment: &pb.TCPFragment{Enable: proto.Bool(fragment), MaxSleepMs: proto.Int32(0)},
		Nonce:       &pb.NoncePattern{Type: pb.NonceType_NONCE_TYPE_RANDOM.Enum()},
	}
}

func main() {
	config := flag.String("config", "", "private official Mieru client JSON file")
	flag.Parse()
	if *config == "" {
		patterns := map[string]string{}
		for name, enabled := range map[string]bool{"baseline": false, "fragment": true} {
			p := pattern(enabled)
			if err := tp.Validate(p); err != nil {
				fail(err)
			}
			patterns[name] = tp.Encode(p)
		}
		json.NewEncoder(os.Stdout).Encode(patterns)
		return
	}
	if err := benchmark(*config); err != nil {
		fail(err)
	}
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

func benchmark(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg pb.ClientConfig
	if err := protojson.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("invalid client JSON: %w", err)
	}
	var profile *pb.ClientProfile
	for _, p := range cfg.Profiles {
		if p.GetProfileName() == cfg.GetActiveProfile() {
			profile = p
			break
		}
	}
	if profile == nil {
		return fmt.Errorf("active profile not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	client := mclient.NewClient()
	if err := client.Store(&mclient.ClientConfig{Profile: profile}); err != nil {
		return err
	}
	if err := client.Start(); err != nil {
		return err
	}
	defer client.Stop()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
		return client.DialContext(ctx, targetAddr(address))
	}, DisableKeepAlives: true, TLSHandshakeTimeout: 10 * time.Second}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 20 * time.Second}
	failures := 0
	for round := 1; round <= 3; round++ {
		for _, url := range []string{"https://www.google.com/generate_204", "https://github.com/robots.txt", "https://www.cloudflare.com/cdn-cgi/trace"} {
			start := time.Now()
			var ttfb time.Duration
			trace := &httptrace.ClientTrace{GotFirstResponseByte: func() { ttfb = time.Since(start) }}
			req, _ := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, url, nil)
			resp, err := httpClient.Do(req)
			row := map[string]any{"round": round, "url": url, "profile": profile.GetProfileName()}
			if err != nil {
				row["error"] = err.Error()
				failures++
			} else {
				n, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, (1<<20)+1))
				resp.Body.Close()
				row["status"] = resp.StatusCode
				row["bytes"] = n
				row["ttfb_ms"] = float64(ttfb.Microseconds()) / 1000
				if readErr != nil {
					row["error"] = readErr.Error()
					failures++
				}
				if resp.StatusCode < 200 || resp.StatusCode >= 400 || n > 1<<20 {
					failures++
					row["check_failed"] = true
				}
			}
			row["total_ms"] = float64(time.Since(start).Microseconds()) / 1000
			json.NewEncoder(os.Stdout).Encode(row)
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d failed checks", failures)
	}
	return nil
}
