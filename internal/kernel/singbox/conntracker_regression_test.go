package singbox

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/buf"
	singM "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"golang.org/x/time/rate"
)

type safeConn struct {
	testConn
	closedFlag atomic.Bool
}

func (c *safeConn) Close() error { c.closedFlag.Store(true); return nil }

type testPacketConn struct {
	closed atomic.Bool
}

func (c *testPacketConn) ReadPacket(b *buf.Buffer) (singM.Socksaddr, error) {
	_, err := b.Write([]byte("ping"))
	return singM.Socksaddr{}, err
}
func (c *testPacketConn) WritePacket(b *buf.Buffer, _ singM.Socksaddr) error {
	b.Release()
	return nil
}
func (c *testPacketConn) Close() error                     { c.closed.Store(true); return nil }
func (c *testPacketConn) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (c *testPacketConn) SetDeadline(time.Time) error      { return nil }
func (c *testPacketConn) SetReadDeadline(time.Time) error  { return nil }
func (c *testPacketConn) SetWriteDeadline(time.Time) error { return nil }

func limitTracker(limit int) *ConnTracker {
	tracker := NewConnTracker(0)
	tracker.SetUserMap(map[string]int{"uuid-1": 1, "uuid-2": 2})
	tracker.SetDeviceLimitFunc(func(uuid string) (int, bool) {
		if uuid == "uuid-1" {
			return limit, true
		}
		return 0, false
	})
	return tracker
}

// Regression: with the slot taken, a new IP that sorts lexicographically
// before the existing one was admitted, so the limit could be exceeded.
func TestConnTrackerRejectsLexicographicallySmallerIPOverLimit(t *testing.T) {
	tracker := limitTracker(1)
	first := &safeConn{}
	if tracker.RoutedConnection(context.Background(), first, testInboundContext("uuid-1", "9.9.9.9"), nil, nil) == first {
		t.Fatal("first device should be admitted")
	}
	for _, ip := range []string{"1.1.1.1", "0.0.0.1", "2001:db8::1"} {
		c := &safeConn{}
		tracker.RoutedConnection(context.Background(), c, testInboundContext("uuid-1", ip), nil, nil)
		if !c.closedFlag.Load() {
			t.Fatalf("device %s must be rejected while the only slot is used", ip)
		}
	}
	pc := &testPacketConn{}
	tracker.RoutedPacketConnection(context.Background(), pc, testInboundContext("uuid-1", "1.1.1.1"), nil, nil)
	if !pc.closed.Load() {
		t.Fatal("UDP from a new device must also be rejected")
	}
}

// Regression: the limit was checked and the IP registered in two separate
// steps, so concurrent connections from new IPs could all pass.
func TestConnTrackerConcurrentNewIPsRespectLimit(t *testing.T) {
	tracker := limitTracker(3)
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := &safeConn{}
			tracker.RoutedConnection(context.Background(), c, testInboundContext("uuid-1", fmt.Sprintf("203.0.113.%d", i)), nil, nil)
			if !c.closedFlag.Load() {
				admitted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if got := admitted.Load(); got != 3 {
		t.Fatalf("admitted %d devices, want exactly 3", got)
	}
}

// Regression: removing a user (or changing its UUID) left its established
// TCP and UDP connections running; CloseByUUID was a no-op.
func TestConnTrackerUserRemovalClosesConnections(t *testing.T) {
	tracker := NewConnTracker(0)
	tracker.SetUserMap(map[string]int{"uuid-1": 1, "uuid-2": 2, "uuid-3": 3})

	tcp := &safeConn{}
	udp := &testPacketConn{}
	keep := &safeConn{}
	rotated := &safeConn{}
	tracker.RoutedConnection(context.Background(), tcp, testInboundContext("uuid-1", "1.1.1.1"), nil, nil)
	tracker.RoutedPacketConnection(context.Background(), udp, testInboundContext("uuid-1", "1.1.1.1"), nil, nil)
	tracker.RoutedConnection(context.Background(), keep, testInboundContext("uuid-2", "2.2.2.2"), nil, nil)
	tracker.RoutedConnection(context.Background(), rotated, testInboundContext("uuid-3", "3.3.3.3"), nil, nil)

	// uuid-1 removed, user 3 got a new UUID.
	tracker.SetUserMap(map[string]int{"uuid-2": 2, "uuid-3b": 3})

	if !tcp.closedFlag.Load() || !udp.closed.Load() {
		t.Fatal("TCP and UDP connections of a removed user must be closed")
	}
	if !rotated.closedFlag.Load() {
		t.Fatal("connections using a replaced UUID must be closed")
	}
	if keep.closedFlag.Load() {
		t.Fatal("connections of remaining users must stay open")
	}
	_, aliveIPs, connCount := tracker.GetUserTraffic()
	if connCount != 1 || aliveIPs[1] != nil {
		t.Fatalf("after removal connCount=%d aliveIPs=%v, want only user 2", connCount, aliveIPs)
	}

	late := &safeConn{}
	tracker.RoutedConnection(context.Background(), late, testInboundContext("uuid-1", "1.1.1.1"), nil, nil)
	if !late.closedFlag.Load() {
		t.Fatal("a handshake that authenticated before removal must be rejected afterwards")
	}

	tracker.SetUserMap(map[string]int{"uuid-1": 1, "uuid-2": 2})
	again := &safeConn{}
	tracker.RoutedConnection(context.Background(), again, testInboundContext("uuid-1", "1.1.1.1"), nil, nil)
	if again.closedFlag.Load() {
		t.Fatal("a re-added user must be able to connect again")
	}
	if n := tracker.CloseByUUID("uuid-1"); n != 1 || !again.closedFlag.Load() {
		t.Fatalf("CloseByUUID() = %d, want 1 and the connection closed", n)
	}
}

func TestConnTrackerPrunesDrainedRemovedUsers(t *testing.T) {
	tracker := NewConnTracker(0)
	tracker.SetUserMap(map[string]int{"uuid-1": 1, "uuid-2": 2})
	c := tracker.RoutedConnection(context.Background(), &safeConn{testConn: testConn{reads: [][]byte{[]byte("abc")}}}, testInboundContext("uuid-1", "1.1.1.1"), nil, nil)
	if _, err := c.Read(make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	tracker.SetUserMap(map[string]int{"uuid-2": 2})

	// Final counters of the removed user are still reported once.
	traffic, _, _ := tracker.GetUserTraffic()
	if traffic[1] != [2]int64{3, 0} {
		t.Fatalf("final traffic of removed user = %v, want [3 0]", traffic[1])
	}
	tracker.usersMu.RLock()
	_, kept := tracker.users[1]
	tracker.usersMu.RUnlock()
	if kept {
		t.Fatal("stats of a drained removed user should be pruned")
	}
}

// Regression: the zero-copy UDP path counted inbound reads as download and
// inbound writes as upload, the reverse of ReadPacket/WritePacket.
func TestTrackedPacketConnUnwrapCountsDirection(t *testing.T) {
	tracker := NewConnTracker(0)
	tracker.SetUserMap(map[string]int{"uuid-1": 1})
	pc := tracker.RoutedPacketConnection(context.Background(), &testPacketConn{}, testInboundContext("uuid-1", "1.1.1.1"), nil, nil).(*trackedPacketConn)

	_, readCounters := pc.UnwrapPacketReader()
	_, writeCounters := pc.UnwrapPacketWriter()
	readCounters[0](100)
	writeCounters[0](7)

	b := buf.New()
	if _, err := pc.ReadPacket(b); err != nil {
		t.Fatal(err)
	}
	b.Release()
	out := buf.New()
	out.Write([]byte("pong!"))
	if err := pc.WritePacket(out, singM.Socksaddr{}); err != nil {
		t.Fatal(err)
	}

	traffic, _, _ := tracker.GetUserTraffic()
	if got := traffic[1]; got != [2]int64{104, 12} {
		t.Fatalf("traffic = %v, want upload 104 (100 zero-copy + 4 read), download 12 (7 + 5 written)", got)
	}
}

// Regression: with a speed limit, Write truncated the buffer to the burst
// size and returned a short count without error, which io.Copy treats as
// io.ErrShortWrite and aborts the connection.
func TestTrackedConnRateLimitedWriteIsComplete(t *testing.T) {
	tracker := NewConnTracker(0)
	tracker.SetUserMap(map[string]int{"uuid-1": 1})
	tracker.SetSpeedLimitFunc(func(string) *rate.Limiter { return rate.NewLimiter(rate.Inf, 4) })
	base := &safeConn{}
	c := tracker.RoutedConnection(context.Background(), base, testInboundContext("uuid-1", "1.1.1.1"), nil, nil)

	n, err := c.Write([]byte("0123456789"))
	if err != nil || n != 10 {
		t.Fatalf("Write() = (%d, %v), want (10, nil)", n, err)
	}
	var got []byte
	for _, w := range base.writes {
		got = append(got, w...)
	}
	if string(got) != "0123456789" {
		t.Fatalf("written bytes = %q", got)
	}
	traffic, _, _ := tracker.GetUserTraffic()
	if traffic[1][1] != 10 {
		t.Fatalf("download = %d, want 10", traffic[1][1])
	}
}

type plainInbound struct{ typ string }

func (p plainInbound) Start(adapter.StartStage) error { return nil }
func (p plainInbound) Close() error                   { return nil }
func (p plainInbound) Type() string                   { return p.typ }
func (p plainInbound) Tag() string                    { return p.typ + "-in" }

// Regression: inbounds without a user update API in the fork (mieru,
// hysteria v1) were treated as successfully updated, so user changes were
// silently dropped until a full restart.
func TestUpdateInboundUsersReportsUnsupportedInbound(t *testing.T) {
	for _, typ := range []string{"mieru", "hysteria"} {
		handled, err := updateInboundUsers(plainInbound{typ}, option.Inbound{Type: typ, Tag: typ + "-in"})
		if handled || err != nil {
			t.Fatalf("%s: updateInboundUsers() = (%v, %v), want (false, nil) so the inbound is recreated", typ, handled, err)
		}
	}
}

// Regression (found in deployment testing): sing's copy loop resolves counters
// with N.UnwrapCountPacketWriter, which skipped a replaceable wrapper before
// asking it for counters, so UDP download bytes were never reported.
func TestTrackedPacketConnCountersSurviveSingUnwrap(t *testing.T) {
	tracker := NewConnTracker(0)
	tracker.SetUserMap(map[string]int{"uuid-1": 1})
	inner := &testPacketConn{}
	pc := tracker.RoutedPacketConnection(context.Background(), inner, testInboundContext("uuid-1", "1.1.1.1"), nil, nil)

	w, writeCounters := N.UnwrapCountPacketWriter(pc, nil)
	r, readCounters := N.UnwrapCountPacketReader(pc, nil)
	if len(writeCounters) != 1 || len(readCounters) != 1 {
		t.Fatalf("counters lost by sing unwrap: write=%d read=%d", len(writeCounters), len(readCounters))
	}
	if w != N.PacketWriter(inner) || r != N.PacketReader(inner) {
		t.Fatal("zero-copy path should still reach the inner conn")
	}
	writeCounters[0](50)
	readCounters[0](20)
	traffic, _, _ := tracker.GetUserTraffic()
	if traffic[1] != [2]int64{20, 50} {
		t.Fatalf("traffic = %v, want upload 20 download 50", traffic[1])
	}
}
