package singbox

import (
	"fmt"
	"net/netip"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/cedar2025/xboard-node/internal/model"
	"github.com/cedar2025/xboard-node/internal/nlog"
	"golang.org/x/net/idna"
)

// All messages use positions, never raw match values or resolver credentials.
func routeWarning(node *model.NodeSpec, index int, reason string) {
	nlog.Core().Warn("panel route item skipped; skipped policy is NOT enforced", "protocol", node.Protocol, "port", node.ServerPort, "route_index", index+1, "reason", reason)
}

func panelMatchers(values []string, dns bool, warn func(string)) ([]M, bool) {
	groups := map[string][]string{}
	var order []string
	wildcard, private := false, false
	add := func(k, v string) {
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		for _, s := range groups[k] {
			if s == v {
				return
			}
		}
		groups[k] = append(groups[k], v)
	}
	for _, line := range values {
		for _, value := range strings.Split(line, "\n") {
			s := strings.TrimSpace(value)
			if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, ";") {
				continue
			}
			if s == "*" || s == "*.*" || (dns && (s == "0.0.0.0/0" || s == "::/0")) {
				wildcard = true
				continue
			}
			prefix, err := netip.ParsePrefix(s)
			if err != nil {
				if ip, e := netip.ParseAddr(s); e == nil {
					prefix = netip.PrefixFrom(ip, ip.BitLen())
					err = nil
				}
			}
			if err == nil {
				if dns {
					warn("DNS IP matcher cannot identify query domains")
				} else {
					add("ip_cidr", prefix.Masked().String())
				}
				continue
			}
			if s == "geoip:private" && !dns {
				private = true
				continue
			}
			if strings.HasPrefix(s, "geoip:") || strings.HasPrefix(s, "geosite:") || strings.HasPrefix(s, "geoip-") || strings.HasPrefix(s, "geosite-") {
				warn("legacy geo database matcher unsupported; use rule-set tag")
				continue
			}
			k, v := "domain_suffix", strings.TrimPrefix(strings.TrimPrefix(s, "*."), ".")
			for _, p := range []struct{ prefix, field string }{{"domain:", "domain_suffix"}, {"full:", "domain"}, {"keyword:", "domain_keyword"}, {"regexp:", "domain_regex"}, {"rule-set:", "rule_set"}} {
				if strings.HasPrefix(s, p.prefix) {
					k, v = p.field, strings.TrimPrefix(s, p.prefix)
					break
				}
			}
			if k == "domain_suffix" && strings.Contains(v, "*") {
				k = "domain_regex"
				v = "^" + strings.ReplaceAll(regexp.QuoteMeta(v), `\*`, ".*") + "$"
			}
			if v == "" {
				warn("empty matcher")
				continue
			}
			if k == "domain_suffix" || k == "domain" {
				v, err = idna.Lookup.ToASCII(strings.TrimSuffix(v, "."))
				v = strings.ToLower(v)
				if err != nil || !regexp.MustCompile(`^[a-z0-9_-]+(?:\.[a-z0-9_-]+)*$`).MatchString(v) {
					warn("invalid domain or IP matcher")
					continue
				}
			}
			if k == "domain_regex" {
				if _, err := regexp.Compile(v); err != nil {
					warn("invalid regular expression")
					continue
				}
			}
			add(k, v)
		}
	}
	var result []M
	for _, k := range order {
		result = append(result, M{k: groups[k]})
	}
	if private {
		result = append(result, M{"ip_is_private": true})
	}
	return result, wildcard
}

const panelDNSPrefix = "xboard-panel-dns-"

func panelDNSServer(address, tag, bootstrap string) (M, error) {
	bad := fmt.Errorf("invalid DNS address, scheme or port")
	if address == "local" || address == "localhost" {
		return M{"type": "local", "tag": tag}, nil
	}
	if ip, err := netip.ParseAddr(strings.Trim(address, "[]")); err == nil {
		return M{"type": "udp", "tag": tag, "server": ip.String()}, nil
	}
	if !strings.Contains(address, "://") {
		address = "udp://" + address
	}
	u, err := url.Parse(address)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" {
		return nil, bad
	}
	switch u.Scheme {
	case "udp", "tcp", "tls", "https", "quic":
	default:
		return nil, bad
	}
	if u.Scheme != "https" && u.Path != "" && u.Path != "/" {
		return nil, bad
	}
	host := u.Hostname()
	if strings.Contains(host, ":") {
		if _, e := netip.ParseAddr(host); e != nil {
			return nil, bad
		}
	}
	result := M{"type": u.Scheme, "tag": tag, "server": host}
	if u.Port() != "" {
		p, e := strconv.Atoi(u.Port())
		if e != nil || p < 1 || p > 65535 {
			return nil, bad
		}
		result["server_port"] = p
	}
	if _, err := netip.ParseAddr(host); err != nil {
		ascii, e := idna.Lookup.ToASCII(host)
		if e != nil || !regexp.MustCompile(`^[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+)*$`).MatchString(ascii) {
			return nil, bad
		}
		result["server"] = ascii
		result["domain_resolver"] = bootstrap
	}
	if u.Scheme == "https" || u.Scheme == "tls" || u.Scheme == "quic" {
		result["tls"] = M{"enabled": true}
	}
	if u.Scheme == "https" {
		path := u.Path
		if path == "" || path == "/" {
			path = "/dns-query"
		}
		result["path"] = path
	}
	return result, nil
}

func mapEntries(value any) []M {
	var result []M
	switch v := value.(type) {
	case []M:
		return v
	case []any:
		for _, item := range v {
			switch m := item.(type) {
			case map[string]any:
				result = append(result, M(m))
			}
		}
	}
	return result
}

func applyPanelRoutes(cfg M, node *model.NodeSpec) {
	if len(node.Routes) == 0 {
		return
	}
	route := cfg["route"].(M)
	tags := map[string]bool{}
	for _, o := range append(mapEntries(cfg["outbounds"]), mapEntries(cfg["endpoints"])...) {
		if t, ok := o["tag"].(string); ok {
			tags[t] = true
		}
	}
	sets := map[string]bool{}
	for _, r := range mapEntries(route["rule_set"]) {
		if t, ok := r["tag"].(string); ok {
			sets[t] = true
		}
	}
	dns := M{}
	switch d := cfg["dns"].(type) {
	case M:
		dns = d
	}
	occupied := map[string]bool{}
	for _, server := range mapEntries(dns["servers"]) {
		if t, ok := server["tag"].(string); ok {
			occupied[t] = true
		}
	}
	// Generate collision-free tags rather than overriding local resolver definitions.
	unique := func(base string) string {
		s := base
		for i := 1; occupied[s]; i++ {
			s = fmt.Sprintf("%s-%d", base, i)
		}
		occupied[s] = true
		return s
	}
	bootstrap := unique(panelDNSPrefix + "system")
	var traffic, resolvers, dnsRules, defaults []M
	scope := func(m M) M {
		r := M{"inbound": []string{node.Protocol + "-in"}}
		for k, v := range m {
			r[k] = v
		}
		return r
	}
	for index, pr := range node.Routes {
		warn := func(reason string) { routeWarning(node, index, reason) }
		action := strings.ToLower(strings.TrimSpace(pr.Action))
		switch action {
		case "block", "reject", "direct", "proxy", "dns":
		default:
			warn("unknown action")
			continue
		}
		matches, all := panelMatchers(pr.Match, action == "dns", warn)
		var valid []M
		for _, m := range matches {
			if list, ok := m["rule_set"].([]string); ok {
				var filtered []string
				for _, tag := range list {
					if sets[tag] {
						filtered = append(filtered, tag)
					} else {
						warn("undefined rule-set tag")
					}
				}
				if len(filtered) == 0 {
					continue
				}
				m["rule_set"] = filtered
			}
			valid = append(valid, m)
		}
		matches = valid
		if len(matches) == 0 && !all {
			continue
		}
		if action == "dns" {
			value := strings.TrimSpace(pr.ActionValue)
			if strings.HasPrefix(strings.ToLower(value), "dns:") {
				value = strings.TrimSpace(value[4:])
			}
			if strings.HasPrefix(strings.ToLower(value), "dns：") {
				value = strings.TrimSpace(value[len("dns："):])
			}
			addresses := strings.FieldsFunc(value, func(r rune) bool { return strings.ContainsRune(",，;； \t\r\n", r) })
			first := ""
			count := 0
			for n, address := range addresses {
				tag := unique(fmt.Sprintf("%s%d-%d", panelDNSPrefix, index, n))
				server, err := panelDNSServer(address, tag, bootstrap)
				if err != nil {
					warn("invalid DNS address")
					continue
				}
				resolvers = append(resolvers, server)
				count++
				if first == "" {
					first = tag
				}
			}
			if first == "" {
				warn("no usable DNS server")
				continue
			}
			if count > 1 {
				nlog.Core().Warn("panel DNS uses first valid server; remaining servers are not automatic failover", "port", node.ServerPort, "route_index", index+1)
			}
			for _, m := range matches {
				r := scope(m)
				r["action"] = "route"
				r["server"] = first
				dnsRules = append(dnsRules, r)
			}
			if all {
				r := scope(M{"action": "route", "server": first})
				defaults = append(defaults, r)
			}
			continue
		}
		target := M{"action": "reject"}
		if action == "direct" || action == "proxy" {
			tag := "direct"
			if action == "proxy" {
				tag = strings.TrimSpace(pr.ActionValue)
			}
			if !tags[tag] {
				warn("undefined proxy outbound tag")
				continue
			}
			target = M{"action": "route", "outbound": tag}
		}
		if all {
			matches = []M{{}}
		}
		for _, m := range matches {
			r := scope(m)
			for k, v := range target {
				r[k] = v
			}
			traffic = append(traffic, r)
		}
	}
	var resolving []M
	if len(resolvers) > 0 {
		servers := append(mapEntries(dns["servers"]), M{"type": "local", "tag": bootstrap})
		dns["servers"] = append(servers, resolvers...)
		dns["rules"] = append(append(dnsRules, defaults...), mapEntries(dns["rules"])...)
		if _, ok := dns["final"]; !ok {
			dns["final"] = bootstrap
		}
		dns["independent_cache"] = true
		cfg["dns"] = dns
		if _, ok := route["default_domain_resolver"]; !ok {
			route["default_domain_resolver"] = dns["final"]
		}
		resolving = []M{scope(M{"action": "resolve"})}
	}
	if len(traffic) == 0 && len(resolving) == 0 {
		return
	}
	// Keep the pre-existing SSRF protection ahead of terminal panel policies.
	guards := ssrfGuardRules()
	var local []M
	for _, r := range mapEntries(route["rules"]) {
		if isGuardRule(r, guards) {
			continue
		}
		local = append(local, r)
	}
	route["rules"] = append(append(append(resolving, guards...), traffic...), local...)
}

func isGuardRule(r M, guards []M) bool {
	for _, g := range guards {
		if reflect.DeepEqual(r, g) {
			return true
		}
	}
	return false
}
