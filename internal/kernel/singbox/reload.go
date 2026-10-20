package singbox

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cedar2025/xboard-node/internal/kernel"
	"github.com/cedar2025/xboard-node/internal/model"
	"github.com/cedar2025/xboard-node/internal/nlog"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	singJSON "github.com/sagernet/sing/common/json"
)

// These objects share references and lifecycle state. Rebuild them together,
// rather than updating routes while leaving their outbounds/resolvers stale.
func runtimeGraph(data []byte) []byte {
	var cfg M
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	delete(cfg, "inbounds")
	result, _ := json.Marshal(cfg)
	return result
}

func validateOutboundReferences(cfg M) error {
	tags := map[string]bool{}
	for _, entry := range append(mapEntries(cfg["outbounds"]), mapEntries(cfg["endpoints"])...) {
		tag, _ := entry["tag"].(string)
		if tag != "" && tags[tag] {
			return fmt.Errorf("duplicate outbound/endpoint tag")
		}
		tags[tag] = true
	}
	route, _ := cfg["route"].(M)
	if final, _ := route["final"].(string); final != "" && !tags[final] {
		return fmt.Errorf("route final references an undefined outbound")
	}
	var check func([]M) error
	check = func(rules []M) error {
		for _, r := range rules {
			if tag, _ := r["outbound"].(string); tag != "" && !tags[tag] {
				return fmt.Errorf("route references an undefined outbound")
			}
			if err := check(mapEntries(r["rules"])); err != nil {
				return err
			}
		}
		return nil
	}
	return check(mapEntries(route["rules"]))
}

func prepareRuntime(data []byte) (*box.Box, context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(context.Background())
	ctx = include.Context(ctx)
	opts, err := singJSON.UnmarshalExtendedContext[option.Options](ctx, data)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	instance, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return instance, ctx, cancel, nil
}

// Caller holds s.mu. Only this node's embedded box is replaced; the machine
// process, other nodes, limit callbacks and accumulated traffic tracker survive.
// Existing connections on this node may reconnect when its runtime changes.
func (s *SingBox) replaceRuntimeLocked(data []byte, node *model.NodeSpec, users []model.UserSpec, tls kernel.TLSCert) error {
	instance, ctx, cancel, err := prepareRuntime(data)
	if err != nil {
		return fmt.Errorf("prepare runtime reload: %w", err)
	}

	// Keep the last known-good graph, but include the most recent user updates.
	var oldConfig M
	if err = json.Unmarshal(s.appliedJSON, &oldConfig); err != nil {
		instance.Close()
		cancel()
		return fmt.Errorf("missing rollback configuration: %w", err)
	}
	if inbound := buildInbound(s.nodeConfig, s.users, s.tls); inbound != nil {
		oldConfig["inbounds"] = []M{inbound}
	} else {
		delete(oldConfig, "inbounds")
	}
	rollbackData, err := json.Marshal(oldConfig)
	if err != nil {
		instance.Close()
		cancel()
		return err
	}

	s.box.Close()
	s.cancel()
	s.box = nil
	s.ctx = nil
	s.cancel = nil
	if err = instance.Start(); err != nil {
		instance.Close()
		cancel()
		previous, previousCtx, previousCancel, restoreErr := prepareRuntime(rollbackData)
		if restoreErr == nil {
			restoreErr = previous.Start()
			if restoreErr != nil {
				previous.Close()
				previousCancel()
			}
		}
		if restoreErr != nil {
			return fmt.Errorf("runtime reload failed: %v; rollback failed: %w", err, restoreErr)
		}
		s.box, s.ctx, s.cancel = previous, previousCtx, previousCancel
		s.trackerRegistered = false
		s.registerTracker(previousCtx)
		return fmt.Errorf("runtime reload failed, previous configuration restored: %w", err)
	}
	s.box, s.ctx, s.cancel = instance, ctx, cancel
	s.nodeConfig, s.users, s.tls = node, users, tls
	s.appliedJSON = append([]byte(nil), data...)
	s.connTracker.SetUserMap(buildUserMap(users))
	s.trackerRegistered = false
	s.registerTracker(ctx)
	nlog.Core().Info("sing-box node runtime reloaded (outbounds/DNS/routes); other nodes unchanged", "port", node.ServerPort)
	return nil
}
