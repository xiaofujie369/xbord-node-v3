package singbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	singLog "github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/auth"
	singJSON "github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/service"
	"golang.org/x/time/rate"

	"github.com/cedar2025/xboard-node/internal/config"
	"github.com/cedar2025/xboard-node/internal/kernel"
	"github.com/cedar2025/xboard-node/internal/model"
	"github.com/cedar2025/xboard-node/internal/nlog"
)

// drainTimeout is how long stop() waits for in-flight connections to finish
// naturally after closing all listen sockets, before hard-killing them.
const drainTimeout = 5 * time.Second

// SingBox implements kernel.Kernel by embedding sing-box as a Go library.
//
// User operations (AddUsers/RemoveUsers/UpdateUsers) use sing-box's native
// UpdatableInbound interface, which hot-swaps user credentials without
// restarting listeners — zero connection disruption.
type SingBox struct {
	cfg config.KernelConfig

	mu     sync.RWMutex
	box    *box.Box
	ctx    context.Context
	cancel context.CancelFunc

	users       []model.UserSpec
	nodeConfig  *model.NodeSpec
	tls         kernel.TLSCert
	appliedJSON []byte // last successfully applied full configuration

	// connTracker is our lightweight in-process byte/IP tracker.
	// Created fresh on every Start (full restart).
	// Survives Reload (hot-swap) since live connections persist.
	connTracker *ConnTracker

	// speedLimitFunc resolves a user UUID to a *rate.Limiter.
	// Set once by SetSpeedLimitFunc and forwarded to every new ConnTracker.
	speedLimitFunc func(string) *rate.Limiter

	// deviceLimitFunc resolves a user UUID to (limit, hasLimit) for gate-keeping.
	// Set once by SetDeviceLimitFunc and forwarded to every new ConnTracker.
	deviceLimitFunc func(string) (int, bool)

	// trackerRegistered prevents duplicate AppendTracker calls on the same
	// Router instance during Reload. Reset to false on full restart.
	trackerRegistered bool
}

func New(cfg config.KernelConfig) *SingBox {
	return &SingBox{cfg: cfg}
}

var _ kernel.Kernel = (*SingBox)(nil)

func (s *SingBox) Name() string { return "sing-box" }

func (s *SingBox) Capabilities() kernel.Capabilities {
	return kernel.Capabilities{
		PerUserSpeedLimit:    true,
		DeviceLimit:          true,
		BuiltInTrafficStats:  false,
		AliveIPTracking:      true,
		ForceCloseConnection: false,
		ForceCloseUser:       true,
	}
}

func (s *SingBox) Protocols() []string {
	return []string{
		"vmess", "vless", "trojan", "shadowsocks",
		"hysteria", "hysteria2", "tuic", "naive", "socks", "http", "anytls", "mieru",
	}
}

func (s *SingBox) Start(nodeConfig *model.NodeSpec, users []model.UserSpec, tls kernel.TLSCert) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfgMap := buildConfig(s.cfg, nodeConfig, users, tls)
	if err := validateOutboundReferences(cfgMap); err != nil {
		return err
	}
	data, err := json.Marshal(cfgMap)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	nlog.Core().Debug("sing-box config generated", "len", len(data))

	ctx, cancel := context.WithCancel(context.Background())
	ctx = include.Context(ctx)

	opts, err := singJSON.UnmarshalExtendedContext[option.Options](ctx, data)
	if err != nil {
		cancel()
		return fmt.Errorf("parse sing-box options: %w", err)
	}

	// Save old state before creating the new instance.
	oldBox := s.box
	oldCancel := s.cancel
	oldCtx := s.ctx
	oldTracker := s.connTracker

	instance, err := box.New(box.Options{
		Context: ctx,
		Options: opts,
	})
	if err != nil {
		cancel()
		return fmt.Errorf("create sing-box instance: %w", err)
	}

	// A restart normally binds the same port, so the old listeners must be
	// released before the new instance starts; otherwise Start fails with
	// "address already in use" and the restart fallback never works.
	var rollback []byte
	if oldBox != nil {
		if rollback, err = json.Marshal(buildConfig(s.cfg, s.nodeConfig, s.users, s.tls)); err != nil {
			instance.Close()
			cancel()
			return fmt.Errorf("prepare rollback configuration: %w", err)
		}
		if im := service.FromContext[adapter.InboundManager](oldCtx); im != nil {
			_ = im.Close()
		}
	}

	if err := instance.Start(); err != nil {
		instance.Close()
		cancel()
		if oldBox == nil {
			return fmt.Errorf("start sing-box: %w", err)
		}
		if restoreErr := s.restorePreviousLocked(oldBox, oldCancel, oldTracker, rollback); restoreErr != nil {
			return fmt.Errorf("start sing-box: %v; restoring previous instance failed: %w", err, restoreErr)
		}
		return fmt.Errorf("start sing-box failed, previous instance restored: %w", err)
	}

	// New instance started successfully — swap state.
	s.box = instance
	s.ctx = ctx
	s.cancel = cancel
	s.users = users
	s.nodeConfig = nodeConfig
	s.tls = tls
	s.appliedJSON = append([]byte(nil), data...)

	// Fresh tracker on full restart.
	s.connTracker = NewConnTracker(0)
	s.connTracker.SetUserMap(buildUserMap(users))
	if s.speedLimitFunc != nil {
		s.connTracker.SetSpeedLimitFunc(s.speedLimitFunc)
	}
	if s.deviceLimitFunc != nil {
		s.connTracker.SetDeviceLimitFunc(s.deviceLimitFunc)
	}

	s.trackerRegistered = false
	s.registerTracker(ctx)

	// Recycle old instance in background — drain then close.
	if oldBox != nil {
		go recycleOldBox(oldBox, oldCancel, oldCtx, oldTracker)
	}

	nlog.Core().Debug("sing-box started", "users", len(users))
	return nil
}

// restorePreviousLocked replaces a previous instance whose listeners were
// already closed with a fresh one built from its configuration, keeping the
// previous traffic tracker so no counters are lost.
func (s *SingBox) restorePreviousLocked(oldBox *box.Box, oldCancel context.CancelFunc, oldTracker *ConnTracker, data []byte) error {
	oldBox.Close()
	if oldCancel != nil {
		oldCancel()
	}
	s.box, s.ctx, s.cancel = nil, nil, nil
	instance, ctx, cancel, err := prepareRuntime(data)
	if err != nil {
		return err
	}
	if err := instance.Start(); err != nil {
		instance.Close()
		cancel()
		return err
	}
	s.box, s.ctx, s.cancel = instance, ctx, cancel
	s.connTracker = oldTracker
	s.trackerRegistered = false
	s.registerTracker(ctx)
	return nil
}

// recycleOldBox gracefully shuts down a previous sing-box instance in the
// background. It closes listen sockets first, waits for connections to drain,
// then hard-closes. This avoids blocking the new instance's startup.
func recycleOldBox(oldBox *box.Box, oldCancel context.CancelFunc, oldCtx context.Context, oldTracker *ConnTracker) {
	// Step 1: close listen sockets so no new connections arrive on old ports.
	if im := service.FromContext[adapter.InboundManager](oldCtx); im != nil {
		_ = im.Close()
	}

	// Step 2: drain in-flight connections (best-effort).
	if oldTracker != nil {
		deadline := time.Now().Add(drainTimeout)
		for time.Now().Before(deadline) {
			if oldTracker.ActiveCount() == 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Step 3: hard-close everything.
	oldBox.Close()
	if oldCancel != nil {
		oldCancel()
	}
	nlog.Core().Debug("sing-box: old instance recycled")
}

// Reload updates inbound state in place when the runtime graph is unchanged.
// Outbound, DNS or route graph changes rebuild this node's box as one unit,
// preserving its traffic tracker. Other nodes and the machine process survive.
func (s *SingBox) Reload(nodeConfig *model.NodeSpec, users []model.UserSpec, tls kernel.TLSCert) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.box == nil {
		return fmt.Errorf("not running")
	}

	cfgMap := buildConfig(s.cfg, nodeConfig, users, tls)
	data, err := json.Marshal(cfgMap)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	opts, err := singJSON.UnmarshalExtendedContext[option.Options](s.ctx, data)
	if err != nil {
		return fmt.Errorf("parse options: %w", err)
	}
	if err := validateOutboundReferences(cfgMap); err != nil {
		return err
	}
	if !bytes.Equal(runtimeGraph(s.appliedJSON), runtimeGraph(data)) {
		return s.replaceRuntimeLocked(data, nodeConfig, users, tls)
	}

	router := service.FromContext[adapter.Router](s.ctx)
	if router == nil {
		return fmt.Errorf("router not available")
	}
	prev, err := s.currentOptionsLocked()
	if err != nil {
		return fmt.Errorf("prepare rollback options: %w", err)
	}

	// Update routing rules
	if err := router.UpdateRules(opts.Route.Rules, opts.Route.RuleSet); err != nil {
		return fmt.Errorf("routing reload failed: %w", err)
	}
	nlog.Core().Debug("sing-box routing reloaded")

	// Configuration hash check for inbound reconstruction
	tlsChanged := !bytes.Equal(s.tls.CertPEM, tls.CertPEM) || !bytes.Equal(s.tls.KeyPEM, tls.KeyPEM)
	configChanged := tlsChanged || s.nodeConfig == nil || kernel.ComputeHash(nodeConfig, users) != kernel.ComputeHash(s.nodeConfig, s.users)

	if err := s.applyInboundsLocked(opts, prev, !configChanged); err != nil {
		if rerr := router.UpdateRules(prev.Route.Rules, prev.Route.RuleSet); rerr != nil {
			return fmt.Errorf("%w; restoring previous routes failed: %v", err, rerr)
		}
		return err
	}

	// Trackers remain registered on the Router (which survives ReloadUsers).
	// Only update the user map — do NOT re-register or traffic is double-counted.
	if s.connTracker != nil {
		s.connTracker.SetUserMap(buildUserMap(users))
	}

	nlog.Core().Debug("sing-box reloaded", "users", len(users))
	s.users = users
	s.nodeConfig = nodeConfig
	s.tls = tls
	s.appliedJSON = append([]byte(nil), data...)
	return nil
}

// registerTracker wires the ConnTracker to the current Router exactly once.
// The ConnTracker handles both byte counting and optional rate limiting
// in a single wrapper, so no additional trackers are needed.
func (s *SingBox) registerTracker(ctx context.Context) {
	if s.trackerRegistered {
		return
	}
	router := service.FromContext[adapter.Router](ctx)
	if router == nil {
		return
	}
	if s.connTracker != nil {
		router.AppendTracker(s.connTracker)
	}
	s.trackerRegistered = true
}

func (s *SingBox) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stop()
}

func (s *SingBox) stop() {
	if s.box == nil {
		return
	}

	// Step 1: close all listen sockets so no new connections are accepted.
	// im.Close() sets m.started=false so the subsequent box.Close() call is a no-op
	// for inbounds (avoids double-close), but box.Close() still tears down routers
	// and outbounds.
	if im := service.FromContext[adapter.InboundManager](s.ctx); im != nil {
		_ = im.Close()
	}

	// Step 2: wait for in-flight connections to drain naturally (best-effort).
	if s.connTracker != nil {
		deadline := time.Now().Add(drainTimeout)
		for time.Now().Before(deadline) {
			if s.connTracker.ActiveCount() == 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Step 3: hard-close everything that is still open.
	s.box.Close()
	s.box = nil
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.ctx = nil
}

func (s *SingBox) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.box != nil
}

// connTrackerLocked returns the connTracker under a read-optimised lock.
func (s *SingBox) connTrackerSafe() *ConnTracker {
	s.mu.Lock()
	ct := s.connTracker
	s.mu.Unlock()
	return ct
}

// SetSpeedLimitFunc configures per-user bandwidth throttling.
func (s *SingBox) SetSpeedLimitFunc(fn func(uuid string) *rate.Limiter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.speedLimitFunc = fn
	if s.connTracker != nil {
		s.connTracker.SetSpeedLimitFunc(fn)
	}
}

// SetDeviceLimitFunc configures per-user device limit gate-keeping.
// Connections exceeding the limit are rejected at connect time.
func (s *SingBox) SetDeviceLimitFunc(fn func(uuid string) (int, bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deviceLimitFunc = fn
	if s.connTracker != nil {
		s.connTracker.SetDeviceLimitFunc(fn)
	}
}

// UpdateGlobalDevices updates the global device state from panel (for multi-node).
func (s *SingBox) UpdateGlobalDevices(users map[int][]string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.connTracker != nil {
		s.connTracker.UpdateGlobalDevices(users)
	}
}

// ClearGlobalDevices clears the global device state (on WS disconnect).
func (s *SingBox) ClearGlobalDevices() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.connTracker != nil {
		s.connTracker.ClearGlobalDevices()
	}
}

// ─── User management (non-disruptive) ───────────────────────────────────────

// AddUsers hot-swaps users into running inbounds. Zero connection disruption.
func (s *SingBox) AddUsers(users []model.UserSpec) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.box == nil {
		return 0, fmt.Errorf("not running")
	}

	existing := make(map[int]struct{}, len(s.users))
	for _, u := range s.users {
		existing[u.ID] = struct{}{}
	}
	var toAdd []model.UserSpec
	for _, u := range users {
		if _, dup := existing[u.ID]; !dup {
			toAdd = append(toAdd, u)
		}
	}
	if len(toAdd) == 0 {
		return 0, nil
	}

	merged := append(append([]model.UserSpec{}, s.users...), toAdd...)
	if err := s.reloadInboundsLocked(merged); err != nil {
		return 0, err
	}
	s.users = merged
	return len(toAdd), nil
}

// RemoveUsers hot-swaps users out of running inbounds. Zero connection disruption
// for remaining users.
func (s *SingBox) RemoveUsers(users []model.UserSpec) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.box == nil {
		return 0, fmt.Errorf("not running")
	}

	removeSet := make(map[int]struct{}, len(users))
	for _, u := range users {
		removeSet[u.ID] = struct{}{}
	}
	var kept []model.UserSpec
	removed := 0
	for _, u := range s.users {
		if _, rm := removeSet[u.ID]; rm {
			removed++
		} else {
			kept = append(kept, u)
		}
	}
	if removed == 0 {
		return 0, nil
	}

	if err := s.reloadInboundsLocked(kept); err != nil {
		return 0, err
	}
	s.users = kept
	return removed, nil
}

// UpdateUsers replaces the entire user set atomically via hot-swap.
func (s *SingBox) UpdateUsers(users []model.UserSpec) (added, removed int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.box == nil {
		return 0, 0, fmt.Errorf("not running")
	}

	toAdd, toRemove := kernel.UserDiff(s.users, users)
	added, removed = len(toAdd), len(toRemove)

	if added == 0 && removed == 0 {
		// Only limits may have changed — update tracker map.
		if s.connTracker != nil {
			s.connTracker.SetUserMap(buildUserMap(users))
		}
		s.users = users
		return 0, 0, nil
	}

	if err = s.reloadInboundsLocked(users); err != nil {
		return 0, 0, err
	}
	s.users = users
	return
}

// reloadInboundsLocked hot-swaps inbound users using UpdatableInbound.
// Must be called with s.mu held.
func (s *SingBox) reloadInboundsLocked(users []model.UserSpec) error {
	cfgMap := buildConfig(s.cfg, s.nodeConfig, users, s.tls)
	data, err := json.Marshal(cfgMap)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	opts, err := singJSON.UnmarshalExtendedContext[option.Options](s.ctx, data)
	if err != nil {
		return fmt.Errorf("parse options: %w", err)
	}

	prev, err := s.currentOptionsLocked()
	if err != nil {
		return fmt.Errorf("prepare rollback options: %w", err)
	}
	if err := s.applyInboundsLocked(opts, prev, true); err != nil {
		return err
	}

	if s.connTracker != nil {
		s.connTracker.SetUserMap(buildUserMap(users))
	}

	nlog.Core().Debug("sing-box users hot-swapped", "users", len(users))
	return nil
}

// currentOptionsLocked parses the options of the state that is currently
// applied (node, users and TLS), used to restore inbounds after a failure.
func (s *SingBox) currentOptionsLocked() (option.Options, error) {
	data, err := json.Marshal(buildConfig(s.cfg, s.nodeConfig, s.users, s.tls))
	if err != nil {
		return option.Options{}, err
	}
	return singJSON.UnmarshalExtendedContext[option.Options](s.ctx, data)
}

// applyInboundsLocked brings the running inbounds to opts. With incremental
// set, inbounds whose type is unchanged get their users hot-swapped; others
// are recreated. If any step fails, every inbound already touched is put back
// to prev so a bad update cannot leave the node without its listener.
func (s *SingBox) applyInboundsLocked(opts, prev option.Options, incremental bool) error {
	im := service.FromContext[adapter.InboundManager](s.ctx)
	if im == nil {
		return fmt.Errorf("inbound manager not available")
	}
	router := service.FromContext[adapter.Router](s.ctx)
	if router == nil {
		return fmt.Errorf("router not available")
	}

	var touched []string
	for _, inb := range opts.Inbounds {
		tag := inb.Tag
		touched = append(touched, tag)
		if incremental {
			if existing, ok := im.Get(tag); ok && existing.Type() == inb.Type {
				handled, err := updateInboundUsers(existing, inb)
				if handled && err == nil {
					continue
				}
				if handled {
					nlog.Core().Warn("incremental update failed, recreating inbound", "tag", tag, "error", err)
				} else {
					nlog.Core().Debug("inbound has no user hot-update, recreating", "tag", tag, "type", inb.Type)
				}
			}
		}
		if err := recreateInbound(s.ctx, im, router, inb); err != nil {
			if rerr := restoreInbounds(s.ctx, im, router, prev, touched); rerr != nil {
				return fmt.Errorf("recreate inbound %s: %v; restoring previous inbound failed: %w", tag, err, rerr)
			}
			return fmt.Errorf("recreate inbound %s failed, previous inbound restored: %w", tag, err)
		}
	}
	return nil
}

// recreateInbound removes the inbound first so the new one can bind the same
// port: im.Create cannot atomically swap a listener and would otherwise fail
// with "address already in use". The listen gap is far shorter than a restart.
func recreateInbound(ctx context.Context, im adapter.InboundManager, router adapter.Router, inb option.Inbound) error {
	_ = im.Remove(inb.Tag) // ignore error when tag doesn't exist yet
	logger := singLog.NewNOPFactory().NewLogger(fmt.Sprintf("inbound/%s[%s]", inb.Type, inb.Tag))
	return im.Create(ctx, router, logger, inb.Tag, inb.Type, inb.Options)
}

// restoreInbounds puts the given tags back to their definition in prev,
// removing tags that prev does not define.
func restoreInbounds(ctx context.Context, im adapter.InboundManager, router adapter.Router, prev option.Options, tags []string) error {
	byTag := make(map[string]option.Inbound, len(prev.Inbounds))
	for _, inb := range prev.Inbounds {
		byTag[inb.Tag] = inb
	}
	var errs []error
	for _, tag := range tags {
		old, ok := byTag[tag]
		if !ok {
			_ = im.Remove(tag)
			continue
		}
		if existing, ok := im.Get(tag); ok && existing.Type() == old.Type {
			if handled, err := updateInboundUsers(existing, old); handled && err == nil {
				continue
			}
		}
		if err := recreateInbound(ctx, im, router, old); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", tag, err))
		}
	}
	return errors.Join(errs...)
}

// updateInboundUsers hot-swaps the user list of a running inbound. handled is
// false when the inbound (or its options type) has no user update API in the
// embedded sing-box fork, e.g. mieru and hysteria v1; callers must then
// recreate the inbound. Treating that case as success left removed users
// authorised and new users rejected until the next full restart.
func updateInboundUsers(existing adapter.Inbound, inb option.Inbound) (handled bool, err error) {
	switch v := existing.(type) {
	case adapter.UpdatableInbound[option.VMessUser]:
		if opts, ok := inb.Options.(*option.VMessInboundOptions); ok {
			return true, v.UpdateUsers(opts.Users)
		}
	case adapter.UpdatableInbound[option.VLESSUser]:
		if opts, ok := inb.Options.(*option.VLESSInboundOptions); ok {
			return true, v.UpdateUsers(opts.Users)
		}
	case adapter.UpdatableInbound[option.TrojanUser]:
		if opts, ok := inb.Options.(*option.TrojanInboundOptions); ok {
			return true, v.UpdateUsers(opts.Users)
		}
	case adapter.UpdatableInbound[option.Hysteria2User]:
		if opts, ok := inb.Options.(*option.Hysteria2InboundOptions); ok {
			return true, v.UpdateUsers(opts.Users)
		}
	case adapter.UpdatableShadowsocksInbound:
		if opts, ok := inb.Options.(*option.ShadowsocksInboundOptions); ok {
			return true, v.UpdateUsersByOptions(opts.Users)
		}
	case adapter.UpdatableInbound[option.TUICUser]:
		if opts, ok := inb.Options.(*option.TUICInboundOptions); ok {
			return true, v.UpdateUsers(opts.Users)
		}
	case adapter.UpdatableInbound[option.AnyTLSUser]:
		if opts, ok := inb.Options.(*option.AnyTLSInboundOptions); ok {
			return true, v.UpdateUsers(opts.Users)
		}
	case adapter.UpdatableInbound[option.MieruUser]:
		if opts, ok := inb.Options.(*option.MieruInboundOptions); ok {
			return true, v.UpdateUsers(opts.Users)
		}
	case adapter.UpdatableInbound[auth.User]:
		switch opts := inb.Options.(type) {
		case *option.NaiveInboundOptions:
			return true, v.UpdateUsers(opts.Users)
		case *option.SocksInboundOptions:
			return true, v.UpdateUsers(opts.Users)
		case *option.HTTPMixedInboundOptions:
			return true, v.UpdateUsers(opts.Users)
		}
	}
	return false, nil
}

// ─── Observability ──────────────────────────────────────────────────────────
func (s *SingBox) GetUserTraffic(_ context.Context) (traffic map[int][2]int64, aliveIPs map[int]map[string]bool, connCount int, err error) {
	ct := s.connTrackerSafe()
	if ct == nil {
		return nil, nil, 0, nil
	}
	traffic, aliveIPs, connCount = ct.GetUserTraffic()
	return traffic, aliveIPs, connCount, nil
}

// CloseConnection force-closes a specific connection by its ID.
func (s *SingBox) CloseConnection(_ context.Context, connID string) error {
	ct := s.connTrackerSafe()
	if ct == nil {
		return fmt.Errorf("not running")
	}
	if !ct.CloseByID(connID) {
		nlog.Core().Debug("CloseConnection: connection not found (already closed?)", "id", connID)
	}
	return nil
}

// CloseUserConnections terminates all connections for a user UUID.
func (s *SingBox) CloseUserConnections(_ context.Context, uuid string) error {
	ct := s.connTrackerSafe()
	if ct == nil {
		return nil
	}
	ct.CloseByUUID(uuid)
	return nil
}

// buildUserMap creates a UUID→userID mapping used by ConnTracker to attribute
// connections to the correct user. All sing-box protocols use the user's UUID
// as the inbound name/username, so this covers every protocol.
func buildUserMap(users []model.UserSpec) map[string]int {
	m := make(map[string]int, len(users))
	for _, u := range users {
		m[u.UUID] = u.ID
	}
	return m
}
