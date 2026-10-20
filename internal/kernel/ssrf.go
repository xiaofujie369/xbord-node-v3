package kernel

// Destinations that proxied users must never reach through the node. Both
// kernels block them ahead of panel routes to prevent SSRF into the host,
// its loopback services, cloud metadata endpoints and private networks.
//
// 0.0.0.0/8 and ::/128 are included because connecting to an unspecified
// address reaches local listeners on Linux.
var (
	BlockedIPv4CIDRs = []string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"224.0.0.0/4",
		"240.0.0.0/4",
	}
	BlockedIPv6CIDRs = []string{
		"::/128",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
	}
)

// BlockedIPv4MappedCIDR covers IPv4-mapped IPv6 literals such as
// ::ffff:127.0.0.1 for kernels whose matcher does not unmap them. xray
// normalises mapped addresses to IPv4 itself and rejects this prefix.
const BlockedIPv4MappedCIDR = "::ffff:0:0/96"

// BlockedCIDRs returns a fresh copy of every blocked IPv4 and IPv6 prefix.
func BlockedCIDRs() []string {
	out := make([]string, 0, len(BlockedIPv4CIDRs)+len(BlockedIPv6CIDRs))
	out = append(out, BlockedIPv4CIDRs...)
	return append(out, BlockedIPv6CIDRs...)
}
