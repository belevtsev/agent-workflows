package tickets

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

var errPublicDestination = errors.New("direct ticket imports require public addresses; use a host connector with --stdin")

func readPublicLink(ctx context.Context, link string) (string, error) {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	client := publicTicketClient(net.DefaultResolver.LookupNetIP, dialer.DialContext)
	return readPublicTicket(ctx, link, client)
}

func publicTicketClient(
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) *http.Client {
	// A direct, new connection for each request enforces this policy even
	// after a same-host redirect. The URL still supplies HTTP Host and TLS SNI.
	transport := &http.Transport{
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: 10 * time.Second,
		DialContext: func(ctx context.Context, network, target string) (net.Conn, error) {
			return dialPublicTicket(ctx, network, target, lookup, dial)
		},
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: checkTicketRedirect}
}

func validPublicTicketURL(u *url.URL) bool {
	return isWebLink(u) && u.Hostname() != "" && u.User == nil && u.Opaque == ""
}

func checkTicketRedirect(r *http.Request, via []*http.Request) error {
	if len(via) >= 5 || !validPublicTicketURL(r.URL) || (via[len(via)-1].URL.Scheme == "https" && r.URL.Scheme != "https") {
		return errors.New("unsafe or excessive public ticket redirect; use a host connector with --stdin")
	}
	return nil
}

func dialPublicTicket(ctx context.Context, network, target string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	var addresses []netip.Addr
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		addresses = []netip.Addr{ip}
	} else {
		addresses, err = lookup(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve public ticket destination: %w", err)
		}
	}
	if len(addresses) == 0 {
		return nil, errPublicDestination
	}
	for _, ip := range addresses {
		if !publicTicketAddress(ip) {
			return nil, errPublicDestination
		}
	}
	for _, ip := range addresses {
		// A numeric connection target prevents a second DNS lookup between
		// validation and connect. Try alternatives only from this resolution.
		conn, dialErr := dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
	}
	return nil, err
}

// IsGlobalUnicast still includes some special-purpose addresses. Refer to
// the IANA IPv4 and IPv6 special-purpose registries when updating this list.
var nonPublicTicketPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

var ipv6GlobalUnicast = netip.MustParsePrefix("2000::/3")

func publicTicketAddress(ip netip.Addr) bool {
	if ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || (ip.Is6() && !ipv6GlobalUnicast.Contains(ip)) {
		return false
	}
	for _, prefix := range nonPublicTicketPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
