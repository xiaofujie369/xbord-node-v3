package singbox

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing/common/buf"
	singM "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"golang.org/x/time/rate"

	"github.com/cedar2025/xboard-node/internal/nlog"
)

// ipPool caches ipSnapshot maps to reduce allocations.
var ipPool = sync.Pool{
	New: func() interface{} {
		return make(map[string]struct{}, 16)
	},
}

// ─── Per-user statistics ────────────────────────────────────────────────────

// userStats holds per-user traffic counters and alive IP tracking.
// Traffic counters are lock-free atomics (read on every packet).
// IP tracking uses a lightweight mutex (only touched at connect/disconnect).
type userStats struct {
	upload   atomic.Int64
	download atomic.Int64

	mu        sync.RWMutex   // RWMutex for concurrent reads
	ips       map[string]int // sourceIP → refcount (number of active conns from that IP)
	connCount int            // total active connections
}

// addConn registers a new connection from sourceIP.
func (u *userStats) addConn(sourceIP string) {
	u.mu.Lock()
	u.connCount++
	u.ips[sourceIP]++
	u.mu.Unlock()
}

// removeConn unregisters a connection from sourceIP.
func (u *userStats) removeConn(sourceIP string) {
	u.mu.Lock()
	u.connCount--
	u.ips[sourceIP]--
	if u.ips[sourceIP] <= 0 {
		delete(u.ips, sourceIP)
	}
	u.mu.Unlock()
}

// distinctIPs returns the number of distinct IPs currently connected.
func (u *userStats) distinctIPs() int {
	u.mu.Lock()
	n := len(u.ips)
	u.mu.Unlock()
	return n
}

// ipSnapshot returns a pooled copy of the current IP set.
func (u *userStats) ipSnapshot() map[string]struct{} {
	cp := ipPool.Get().(map[string]struct{})
	for k := range cp {
		delete(cp, k)
	}
	u.mu.Lock()
	for ip := range u.ips {
		cp[ip] = struct{}{}
	}
	u.mu.Unlock()
	return cp
}

// releaseIPSnapshot returns the snapshot to pool.
// Large maps are discarded to prevent memory bloat.
func releaseIPSnapshot(m map[string]struct{}) {
	// Discard large maps to prevent pool memory bloat
	if len(m) > 64 {
		return
	}
	ipPool.Put(m)
}

// aliveIPList returns the set of alive IPs as a boolean map (for reporting).
func (u *userStats) aliveIPList() map[string]bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.ips) == 0 {
		return nil
	}
	result := make(map[string]bool, len(u.ips))
	for ip := range u.ips {
		result[ip] = true
	}
	return result
}

// ─── ConnTracker ────────────────────────────────────────────────────────────

// ConnTracker provides per-user traffic counting, alive IP tracking, device
// limit gate-keeping, and per-user speed limiting for the sing-box kernel.
//
// Architecture: instead of maintaining a per-connection map (O(connections)),
// it maintains per-user atomic counters and IP refcount sets (O(users)).
// This means:
//   - GetUserTraffic is O(users), not O(connections)
//   - No Snapshot/pending buffer needed
//   - No per-connection map lock contention
//   - Bytes are counted directly into per-user atomics via CountFunc callbacks
//
// The per-connection wrapper (trackedConn) is still needed for:
//   - Hooking into sing's ReadCounter/WriteCounter for zero-copy byte counting
//   - Close callback to decrement IP refcounts
//   - Per-connection rate limit token accumulation (amortized WaitN)
type ConnTracker struct {
	usersMu   sync.RWMutex
	users     map[int]*userStats              // userID → stats
	uuidMap   map[string]int                  // UUID → userID (for lookup in RoutedConnection)
	connMap   map[string]io.Closer            // connID → TCP or UDP conn (force-close support)
	userConns map[string]map[string]io.Closer // UUID → connID → conn (close on user removal)
	revoked   map[string]time.Time            // recently removed UUIDs, rejected at admission

	idCounter atomic.Int64

	// speedLimitFunc resolves a user UUID to a *rate.Limiter.
	speedLimitFunc atomic.Pointer[func(uuid string) *rate.Limiter]

	// deviceLimitFunc resolves a user UUID to their device limit.
	deviceLimitFunc atomic.Pointer[func(uuid string) (int, bool)]

	// Multi-node device state from panel
	globalDevices    map[int]map[string]bool // userID → IP → exists
	globalMu         sync.RWMutex
	globalLastUpdate time.Time
}

// NewConnTracker creates a tracker.
func NewConnTracker(_ int) *ConnTracker {
	return &ConnTracker{
		users:         make(map[int]*userStats),
		uuidMap:       make(map[string]int),
		connMap:       make(map[string]io.Closer),
		userConns:     make(map[string]map[string]io.Closer),
		revoked:       make(map[string]time.Time),
		globalDevices: make(map[int]map[string]bool),
	}
}

// SetSpeedLimitFunc configures the per-user speed limit lookup.
func (t *ConnTracker) SetSpeedLimitFunc(fn func(uuid string) *rate.Limiter) {
	t.speedLimitFunc.Store(&fn)
}

// SetDeviceLimitFunc configures the per-user device limit lookup for gate-keeping.
func (t *ConnTracker) SetDeviceLimitFunc(fn func(uuid string) (int, bool)) {
	t.deviceLimitFunc.Store(&fn)
}

// revokedTTL bounds how long a removed UUID is refused at admission. It only
// needs to cover handshakes that authenticated against the old user list.
const revokedTTL = 30 * time.Second

// SetUserMap replaces the UUID→userID mapping and ensures per-user stats
// structs exist for all users. Connections of UUIDs that are no longer
// present are closed: removing a user from the inbound only stops new
// handshakes, it does not end streams that were already authenticated.
// Stats of removed users are kept until the next GetUserTraffic so their
// final bytes are still reported.
func (t *ConnTracker) SetUserMap(m map[string]int) {
	now := time.Now()
	var stale []io.Closer

	t.usersMu.Lock()
	for uuid, uid := range t.uuidMap {
		if newUID, ok := m[uuid]; ok && newUID == uid {
			continue
		}
		t.revoked[uuid] = now
		for _, c := range t.userConns[uuid] {
			stale = append(stale, c)
		}
	}
	for uuid, at := range t.revoked {
		if _, ok := m[uuid]; ok || now.Sub(at) > revokedTTL {
			delete(t.revoked, uuid)
		}
	}
	t.uuidMap = m
	for _, uid := range m {
		if _, ok := t.users[uid]; !ok {
			t.users[uid] = &userStats{ips: make(map[string]int)}
		}
	}
	t.usersMu.Unlock()

	// Close outside the lock: Close re-enters the tracker to unregister.
	for _, c := range stale {
		_ = c.Close()
	}
	if len(stale) > 0 {
		nlog.Core().Info("singbox: closed connections of removed users", "connections", len(stale))
	}
}

// UpdateGlobalDevices syncs global device state from panel.
func (t *ConnTracker) UpdateGlobalDevices(users map[int][]string) {
	t.globalMu.Lock()
	t.globalDevices = make(map[int]map[string]bool, len(users))
	for uid, ips := range users {
		m := make(map[string]bool, len(ips))
		for _, ip := range ips {
			m[ip] = true
		}
		t.globalDevices[uid] = m
	}
	t.globalLastUpdate = time.Now()
	t.globalMu.Unlock()
	nlog.Core().Debug("global device state updated", "users", len(users))
}

// ClearGlobalDevices resets global device state (on WS disconnect).
func (t *ConnTracker) ClearGlobalDevices() {
	t.globalMu.Lock()
	t.globalDevices = make(map[int]map[string]bool)
	t.globalLastUpdate = time.Time{}
	t.globalMu.Unlock()
	nlog.Core().Debug("global device state cleared")
}

// ─── adapter.ConnectionTracker ──────────────────────────────────────────────

// RoutedConnection wraps a TCP conn to count bytes per-user, track IPs,
// and optionally rate-limit. Gate-keeps device limits at connection time.
func (t *ConnTracker) RoutedConnection(
	ctx context.Context, conn net.Conn,
	metadata adapter.InboundContext,
	_ adapter.Rule, _ adapter.Outbound,
) net.Conn {
	uuid := metadata.User
	sourceIP := metadata.Source.Addr.String()

	uid, us, ok := t.admit(uuid, sourceIP, "TCP")
	if !ok {
		conn.Close()
		return conn
	}

	tc := &trackedConn{
		Conn:     conn,
		tracker:  t,
		us:       us,
		userID:   uid,
		uuid:     uuid,
		connID:   t.nextID(),
		sourceIP: sourceIP,
		ctx:      ctx,
	}
	if slf := t.speedLimitFunc.Load(); slf != nil {
		tc.limiter = (*slf)(uuid)
	}
	if !t.register(uuid, tc.connID, tc) {
		// The user was removed while this connection was being admitted.
		tc.Close()
	}
	return tc
}

// admit applies the device limit and registers the source IP in one step,
// so concurrent connections from new IPs cannot all pass the check before
// any of them is counted. It returns ok=false when the connection must be
// rejected.
func (t *ConnTracker) admit(uuid, sourceIP, network string) (uid int, us *userStats, ok bool) {
	t.usersMu.RLock()
	uid = t.uuidMap[uuid]
	us = t.users[uid]
	_, revoked := t.revoked[uuid]
	t.usersMu.RUnlock()

	if revoked && uid == 0 {
		nlog.Core().Info("singbox: rejecting connection of removed user", "network", network, "user", uuid)
		return 0, nil, false
	}
	if us == nil {
		return uid, nil, true
	}

	limit, hasLimit := 0, false
	if dlf := t.deviceLimitFunc.Load(); dlf != nil {
		limit, hasLimit = (*dlf)(uuid)
	}

	us.mu.Lock()
	defer us.mu.Unlock()
	if hasLimit && t.overDeviceLimitLocked(us, uid, sourceIP, limit) {
		nlog.Core().Info("singbox: device limit gate-keep, rejecting connection",
			"network", network, "user", uuid, "ip", sourceIP, "limit", limit)
		return uid, us, false
	}
	us.connCount++
	us.ips[sourceIP]++
	return uid, us, true
}

// register records a live connection for force-close. It refuses UUIDs that
// were removed after admission so a racing handshake cannot outlive removal.
func (t *ConnTracker) register(uuid, connID string, c io.Closer) bool {
	t.usersMu.Lock()
	defer t.usersMu.Unlock()
	if _, revoked := t.revoked[uuid]; revoked {
		if _, present := t.uuidMap[uuid]; !present {
			return false
		}
	}
	t.connMap[connID] = c
	conns := t.userConns[uuid]
	if conns == nil {
		conns = make(map[string]io.Closer)
		t.userConns[uuid] = conns
	}
	conns[connID] = c
	return true
}

// RoutedPacketConnection wraps UDP with per-user counting, device limits and
// force-close support, mirroring RoutedConnection.
func (t *ConnTracker) RoutedPacketConnection(
	ctx context.Context, conn N.PacketConn,
	metadata adapter.InboundContext,
	_ adapter.Rule, _ adapter.Outbound,
) N.PacketConn {
	uuid := metadata.User
	sourceIP := metadata.Source.Addr.String()

	uid, us, ok := t.admit(uuid, sourceIP, "UDP")
	if !ok {
		conn.Close()
		return conn
	}

	pc := &trackedPacketConn{
		PacketConn: conn,
		tracker:    t,
		us:         us,
		userID:     uid,
		uuid:       uuid,
		connID:     t.nextID(),
		sourceIP:   sourceIP,
		ctx:        ctx,
	}
	if slf := t.speedLimitFunc.Load(); slf != nil {
		pc.limiter = (*slf)(uuid)
	}
	if !t.register(uuid, pc.connID, pc) {
		pc.Close()
	}
	return pc
}

// checkDeviceGate reports whether a new connection from sourceIP would exceed
// the device limit, without registering it.
func (t *ConnTracker) checkDeviceGate(us *userStats, userID int, sourceIP string, limit int) bool {
	if us == nil {
		return false
	}
	us.mu.RLock()
	defer us.mu.RUnlock()
	return t.overDeviceLimitLocked(us, userID, sourceIP, limit)
}

// overDeviceLimitLocked decides admission with us.mu held. An IP that already
// has a live connection here, or that the panel reports for this user on
// another node, is never counted twice. A new IP is admitted only while the
// distinct device count (local, plus global when the panel state is fresh)
// is below the limit. Earlier versions admitted any new IP that sorted
// lexicographically before the existing ones, so a user could exceed the
// limit by connecting from "smaller" addresses.
func (t *ConnTracker) overDeviceLimitLocked(us *userStats, userID int, sourceIP string, limit int) bool {
	if limit <= 0 {
		return false
	}
	if us.ips[sourceIP] > 0 {
		return false
	}

	t.globalMu.RLock()
	globalStale := time.Since(t.globalLastUpdate) > 60*time.Second
	globalIPs := t.globalDevices[userID]
	t.globalMu.RUnlock()

	if globalStale || globalIPs == nil {
		if len(us.ips) < limit {
			return false
		}
		nlog.Core().Debug("device limit: local over limit, rejecting",
			"userID", userID, "ip", sourceIP, "localIPs", len(us.ips), "limit", limit)
		return true
	}

	if globalIPs[sourceIP] {
		return false
	}
	devices := len(globalIPs)
	for ip := range us.ips {
		if !globalIPs[ip] {
			devices++
		}
	}
	if devices < limit {
		return false
	}
	nlog.Core().Debug("device limit: total over limit, rejecting",
		"userID", userID, "ip", sourceIP, "totalIPs", devices, "limit", limit)
	return true
}

func (t *ConnTracker) nextID() string {
	return "sb-" + formatInt36(t.idCounter.Add(1))
}

func formatInt36(n int64) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	var buf [13]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%36]
		n /= 36
	}
	return string(buf[i:])
}

// ─── Public API ─────────────────────────────────────────────────────────────

// GetUserTraffic returns per-user cumulative traffic and alive IPs.
// This is O(users), not O(connections).
func (t *ConnTracker) GetUserTraffic() (traffic map[int][2]int64, aliveIPs map[int]map[string]bool, connCount int) {
	t.usersMu.RLock()
	traffic = make(map[int][2]int64, len(t.users))
	aliveIPs = make(map[int]map[string]bool, len(t.users))
	for uid, us := range t.users {
		up := us.upload.Load()
		down := us.download.Load()
		if up > 0 || down > 0 {
			traffic[uid] = [2]int64{up, down}
		}

		ips := us.aliveIPList()
		if ips != nil {
			aliveIPs[uid] = ips
		}

		us.mu.Lock()
		connCount += us.connCount
		us.mu.Unlock()
	}
	t.usersMu.RUnlock()
	t.pruneRemovedUsers()
	return
}

// pruneRemovedUsers drops stats of users that left the user map and have no
// live connections. Their final counters were returned by the caller.
func (t *ConnTracker) pruneRemovedUsers() {
	t.usersMu.Lock()
	defer t.usersMu.Unlock()
	active := make(map[int]bool, len(t.uuidMap))
	for _, uid := range t.uuidMap {
		active[uid] = true
	}
	for uid, us := range t.users {
		if active[uid] {
			continue
		}
		us.mu.Lock()
		idle := us.connCount == 0
		us.mu.Unlock()
		if idle {
			delete(t.users, uid)
		}
	}
}

// CloseByID force-closes a connection by its ID.
func (t *ConnTracker) CloseByID(id string) bool {
	t.usersMu.RLock()
	conn, ok := t.connMap[id]
	t.usersMu.RUnlock()
	if !ok {
		return false
	}
	if conn != nil {
		conn.Close()
	}
	return true
}

// CloseByUUID force-closes all TCP and UDP connections of a user UUID and
// returns how many were closed.
func (t *ConnTracker) CloseByUUID(uuid string) int {
	t.usersMu.RLock()
	conns := make([]io.Closer, 0, len(t.userConns[uuid]))
	for _, c := range t.userConns[uuid] {
		conns = append(conns, c)
	}
	t.usersMu.RUnlock()
	for _, c := range conns {
		_ = c.Close()
	}
	return len(conns)
}

// ActiveCount returns the total number of active connections.
func (t *ConnTracker) ActiveCount() int {
	t.usersMu.RLock()
	total := 0
	for _, us := range t.users {
		us.mu.Lock()
		total += us.connCount
		us.mu.Unlock()
	}
	t.usersMu.RUnlock()
	return total
}

// removeConnRef removes the connection reference from connMap on close.
func (t *ConnTracker) removeConnRef(uuid, connID string) {
	t.usersMu.Lock()
	delete(t.connMap, connID)
	if conns := t.userConns[uuid]; conns != nil {
		delete(conns, connID)
		if len(conns) == 0 {
			delete(t.userConns, uuid)
		}
	}
	t.usersMu.Unlock()
}

// ─── trackedConn (TCP) ──────────────────────────────────────────────────────

// RateLimitedWriter wraps a Writer with non-blocking rate limiting.
// Uses AllowN for instant checks and context-aware waiting when needed.
type RateLimitedWriter struct {
	writer  io.Writer
	limiter *rate.Limiter
	ctx     context.Context
}

// Write implements io.Writer with rate limiting.
func (w *RateLimitedWriter) Write(b []byte) (int, error) {
	// Truncate to burst size
	if burst := w.limiter.Burst(); len(b) > burst {
		b = b[:burst]
	}

	// Fast path: check if tokens available (non-blocking)
	if w.limiter.AllowN(time.Now(), len(b)) {
		return w.writer.Write(b)
	}

	// Slow path: wait for tokens with context cancellation support
	resv := w.limiter.ReserveN(time.Now(), len(b))
	if delay := resv.Delay(); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			// Tokens available, proceed
		case <-w.ctx.Done():
			resv.Cancel()
			return 0, w.ctx.Err()
		}
	}

	return w.writer.Write(b)
}

// RateLimitedReadCloser wraps a Reader with non-blocking rate limiting.
type RateLimitedReadCloser struct {
	reader  io.Reader
	limiter *rate.Limiter
	ctx     context.Context
}

// Read implements io.Reader with rate limiting.
func (r *RateLimitedReadCloser) Read(b []byte) (int, error) {
	// Truncate to burst size
	if burst := r.limiter.Burst(); len(b) > burst {
		b = b[:burst]
	}

	n, err := r.reader.Read(b)
	if n > 0 && r.limiter != nil {
		// Fast path: check if tokens available
		if !r.limiter.AllowN(time.Now(), n) {
			// Slow path: wait with context cancellation
			resv := r.limiter.ReserveN(time.Now(), n)
			if delay := resv.Delay(); delay > 0 {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-timer.C:
					// Tokens available
				case <-r.ctx.Done():
					resv.Cancel()
					return n, r.ctx.Err()
				}
			}
		}
	}
	return n, err
}

type trackedConn struct {
	net.Conn
	tracker  *ConnTracker
	us       *userStats // per-user stats (upload/download atomics + IP tracking)
	userID   int        // user ID for device tracking
	uuid     string
	connID   string
	sourceIP string
	limiter  *rate.Limiter
	ctx      context.Context
	closed   atomic.Bool
}

func (c *trackedConn) Read(b []byte) (int, error) {
	if c.limiter != nil {
		if burst := c.limiter.Burst(); len(b) > burst {
			b = b[:burst]
		}
	}
	n, err := c.Conn.Read(b)
	if n > 0 {
		if c.us != nil {
			c.us.upload.Add(int64(n)) // 从入站读取 = 用户上传
		}
		if c.limiter != nil {
			// Non-blocking rate limiting
			if !c.limiter.AllowN(time.Now(), n) {
				resv := c.limiter.ReserveN(time.Now(), n)
				if delay := resv.Delay(); delay > 0 {
					timer := time.NewTimer(delay)
					defer timer.Stop()
					select {
					case <-timer.C:
						// Tokens available
					case <-c.ctx.Done():
						resv.Cancel()
						return n, c.ctx.Err()
					}
				}
			}
		}
	}
	return n, err
}

func (c *trackedConn) Write(b []byte) (int, error) {
	if c.limiter == nil {
		n, err := c.Conn.Write(b)
		if n > 0 && c.us != nil {
			c.us.download.Add(int64(n)) // 向入站写入 = 用户下载
		}
		return n, err
	}

	// Rate-limited writes go out in burst-sized chunks. io.Writer forbids a
	// short write without an error, so every chunk is written before returning.
	written := 0
	for written < len(b) {
		chunk := b[written:]
		if burst := c.limiter.Burst(); len(chunk) > burst {
			chunk = chunk[:burst]
		}
		if !c.limiter.AllowN(time.Now(), len(chunk)) {
			resv := c.limiter.ReserveN(time.Now(), len(chunk))
			if delay := resv.Delay(); delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-timer.C:
				case <-c.ctx.Done():
					timer.Stop()
					resv.Cancel()
					return written, c.ctx.Err()
				}
			}
		}
		n, err := c.Conn.Write(chunk)
		if n > 0 && c.us != nil {
			c.us.download.Add(int64(n)) // 向入站写入 = 用户下载
		}
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func (c *trackedConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		if c.us != nil {
			c.us.removeConn(c.sourceIP)
		}
		c.tracker.removeConnRef(c.uuid, c.connID)
	}
	return c.Conn.Close()
}

// makeCountFunc builds a CountFunc for zero-copy byte counting via sing's
// ReadCounter/WriteCounter unwrap interfaces.
func (c *trackedConn) makeCountFunc(counter *atomic.Int64) N.CountFunc {
	if c.limiter == nil {
		return func(n int64) { counter.Add(n) }
	}
	return func(n int64) {
		counter.Add(n)
		// Non-blocking rate limiting with context cancellation
		if !c.limiter.AllowN(time.Now(), int(n)) {
			resv := c.limiter.ReserveN(time.Now(), int(n))
			if delay := resv.Delay(); delay > 0 {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-timer.C:
					// Tokens available
				case <-c.ctx.Done():
					resv.Cancel()
				}
			}
		}
	}
}

func (c *trackedConn) UnwrapReader() (io.Reader, []N.CountFunc) {
	if c.us == nil {
		return c.Conn, nil
	}
	return c.Conn, []N.CountFunc{c.makeCountFunc(&c.us.upload)} // 从入站读取 = 用户上传
}

func (c *trackedConn) UnwrapWriter() (io.Writer, []N.CountFunc) {
	if c.us == nil {
		return c.Conn, nil
	}
	return c.Conn, []N.CountFunc{c.makeCountFunc(&c.us.download)} // 向入站写入 = 用户下载
}

func (c *trackedConn) Upstream() any           { return c.Conn }
func (c *trackedConn) ReaderReplaceable() bool { return true }
func (c *trackedConn) WriterReplaceable() bool { return true }

// ─── trackedPacketConn (UDP / QUIC) ─────────────────────────────────────────

type trackedPacketConn struct {
	N.PacketConn
	tracker  *ConnTracker
	us       *userStats
	userID   int
	uuid     string
	connID   string
	sourceIP string
	limiter  *rate.Limiter
	ctx      context.Context
	closed   atomic.Bool
}

func (c *trackedPacketConn) ReadPacket(buffer *buf.Buffer) (singM.Socksaddr, error) {
	dest, err := c.PacketConn.ReadPacket(buffer)
	if err == nil {
		n := int64(buffer.Len())
		if c.us != nil {
			c.us.upload.Add(n) // 从入站读取 = 用户上传
		}
		if c.limiter != nil {
			// Non-blocking rate limiting with context cancellation
			if !c.limiter.AllowN(time.Now(), int(n)) {
				resv := c.limiter.ReserveN(time.Now(), int(n))
				if delay := resv.Delay(); delay > 0 {
					timer := time.NewTimer(delay)
					defer timer.Stop()
					select {
					case <-timer.C:
						// Tokens available
					case <-c.ctx.Done():
						resv.Cancel()
						return dest, c.ctx.Err()
					}
				}
			}
		}
	}
	return dest, err
}

func (c *trackedPacketConn) WritePacket(buffer *buf.Buffer, dest singM.Socksaddr) error {
	n := int64(buffer.Len())

	// Apply rate limiting before write
	if c.limiter != nil {
		if !c.limiter.AllowN(time.Now(), int(n)) {
			resv := c.limiter.ReserveN(time.Now(), int(n))
			if delay := resv.Delay(); delay > 0 {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-timer.C:
					// Tokens available
				case <-c.ctx.Done():
					resv.Cancel()
					return c.ctx.Err()
				}
			}
		}
	}

	err := c.PacketConn.WritePacket(buffer, dest)
	if err == nil && c.us != nil {
		c.us.download.Add(n) // 向入站写入 = 用户下载
	}
	return err
}

func (c *trackedPacketConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		if c.us != nil {
			c.us.removeConn(c.sourceIP)
		}
		c.tracker.removeConnRef(c.uuid, c.connID)
	}
	return c.PacketConn.Close()
}

func (c *trackedPacketConn) makeCountFunc(counter *atomic.Int64) N.CountFunc {
	if c.limiter == nil {
		return func(n int64) { counter.Add(n) }
	}
	return func(n int64) {
		counter.Add(n)
		// Non-blocking rate limiting with context cancellation
		if !c.limiter.AllowN(time.Now(), int(n)) {
			resv := c.limiter.ReserveN(time.Now(), int(n))
			if delay := resv.Delay(); delay > 0 {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				select {
				case <-timer.C:
					// Tokens available
				case <-c.ctx.Done():
					resv.Cancel()
				}
			}
		}
	}
}

func (c *trackedPacketConn) UnwrapPacketReader() (N.PacketReader, []N.CountFunc) {
	if c.us == nil {
		return c.PacketConn, nil
	}
	// Reading from the inbound side is user upload, matching ReadPacket.
	return c.PacketConn, []N.CountFunc{c.makeCountFunc(&c.us.upload)}
}

func (c *trackedPacketConn) UnwrapPacketWriter() (N.PacketWriter, []N.CountFunc) {
	if c.us == nil {
		return c.PacketConn, nil
	}
	// Writing to the inbound side is user download, matching WritePacket.
	return c.PacketConn, []N.CountFunc{c.makeCountFunc(&c.us.download)}
}

func (c *trackedPacketConn) Upstream() any           { return c.PacketConn }
func (c *trackedPacketConn) ReaderReplaceable() bool { return true }

// WriterReplaceable must stay false: sing's UnwrapCountPacketWriter first
// strips replaceable writers via UnwrapPacketWriter and only then looks for
// PacketWriteCounter, so a replaceable wrapper lost its download counter and
// UDP downlink traffic was neither counted nor rate limited. With false, the
// copier still reaches the inner conn through UnwrapPacketWriter above.
func (c *trackedPacketConn) WriterReplaceable() bool { return false }
