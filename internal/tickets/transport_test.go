package tickets

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// Fixture IPs are inputs only. The test dialer connects solely to the local fixture.
var publicFixtureIP = netip.MustParseAddr("93.184.216.34")

func fixturePublicTicket(t *testing.T, server *httptest.Server, lookup func(context.Context, string, string) ([]netip.Addr, error)) func(string) (string, error) {
	t.Helper()
	dialer := net.Dialer{}
	client := publicTicketClient(lookup, func(ctx context.Context, network, target string) (net.Conn, error) {
		if want := net.JoinHostPort(publicFixtureIP.String(), "80"); network != "tcp" || target != want {
			t.Errorf("dialed %s %s, want tcp %s", network, target, want)
			return nil, errors.New("unexpected fixture dial")
		}
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	})
	return func(link string) (string, error) {
		return readPublicTicket(t.Context(), link, client)
	}
}

func TestPublicTicketAddress(t *testing.T) {
	for _, tc := range []struct {
		address string
		allowed bool
	}{
		{"8.8.4.4", true}, {"2606:4700::1111", true}, {"::ffff:8.8.4.4", true},
		{"0.1.2.3", false}, {"10.0.0.4", false}, {"100.64.0.2", false}, {"127.0.0.2", false},
		{"169.254.8.1", false}, {"172.16.0.5", false}, {"192.0.0.8", false}, {"192.0.2.1", false},
		{"192.88.99.1", false}, {"192.168.5.4", false}, {"198.19.0.1", false},
		{"198.51.100.1", false}, {"203.0.113.1", false}, {"224.0.0.1", false}, {"240.0.0.1", false},
		{"255.255.255.255", false}, {"::", false}, {"::1", false}, {"::ffff:192.168.1.1", false},
		{"::127.0.0.1", false}, {"64:ff9b::7f00:1", false}, {"64:ff9b:1::1", false}, {"100::1", false},
		{"2001:2::1", false}, {"2001:db8::1", false}, {"2002::1", false}, {"3fff::1", false},
		{"fc00::1", false}, {"fe80::1", false}, {"fec0::1", false}, {"ff02::1", false},
		{"2606:4700::1111%local", false},
	} {
		if got := publicTicketAddress(netip.MustParseAddr(tc.address)); got != tc.allowed {
			t.Errorf("publicTicketAddress(%s) = %t, want %t", tc.address, got, tc.allowed)
		}
	}
}

func TestPublicTicketDialUsesOnlyValidatedLiterals(t *testing.T) {
	dialFailure := errors.New("fixture dial ended")
	local := netip.MustParseAddr("127.0.0.1")
	ipv6 := netip.MustParseAddr("2606:4700::1111")
	for _, tc := range []struct {
		name, target string
		resolved     []netip.Addr
		dials        []string
		want         error
	}{
		{"resolved public addresses", "tickets.example:8080", []netip.Addr{ipv6, publicFixtureIP},
			[]string{"[2606:4700::1111]:8080", "93.184.216.34:8080"}, dialFailure},
		{"numeric mapped address", "[::ffff:93.184.216.34]:443", nil, []string{"93.184.216.34:443"}, dialFailure},
		{"literal loopback", "127.0.0.1:80", nil, nil, errPublicDestination},
		{"mixed resolution", "tickets.example:80", []netip.Addr{publicFixtureIP, local}, nil, errPublicDestination},
		{"empty resolution", "tickets.example:80", []netip.Addr{}, nil, errPublicDestination},
		{"invalid resolution", "tickets.example:80", []netip.Addr{{}}, nil, errPublicDestination},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved := false
			lookup := func(_ context.Context, network, host string) ([]netip.Addr, error) {
				if tc.resolved == nil || resolved || network != "ip" || host != "tickets.example" {
					t.Fatalf("unexpected resolution %s %s", network, host)
				}
				resolved = true
				return tc.resolved, nil
			}
			var dials []string
			dial := func(_ context.Context, network, target string) (net.Conn, error) {
				if network != "tcp" {
					t.Errorf("unexpected network %s", network)
				}
				dials = append(dials, target)
				return nil, dialFailure
			}
			_, err := dialPublicTicket(t.Context(), "tcp", tc.target, lookup, dial)
			if !errors.Is(err, tc.want) || !slices.Equal(dials, tc.dials) || resolved != (tc.resolved != nil) {
				t.Fatalf("dials %v, resolved %v, error %v; want dials %v and %v", dials, resolved, err, tc.dials, tc.want)
			}
		})
	}
}

func TestPublicTicketChecksEveryRedirect(t *testing.T) {
	for _, tc := range []struct {
		name, redirect string
		want           error
	}{
		{"public destination", "http://next.example/export", nil},
		{"loopback literal", "http://127.0.0.1/export", errPublicDestination},
		{"nonpublic hostname", "http://private.example/export", errPublicDestination},
		{"same host changes address", "/export", errPublicDestination},
		{"credentials", "http://user@next.example/export", nil},
		{"different scheme", "file:///example", nil},
		{"redirect limit", "/ticket", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
			var lookups atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/ticket" {
					http.Redirect(w, r, tc.redirect, http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				_, _ = io.WriteString(w, "Export ticket")
			}))
			defer server.Close()
			read := fixturePublicTicket(t, server, func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
				count := lookups.Add(1)
				if host == "private.example" || tc.name == "same host changes address" && count > 1 {
					return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
				}
				return []netip.Addr{publicFixtureIP}, nil
			})
			body, err := read("http://tickets.example/ticket")
			switch {
			case tc.name == "public destination":
				if err != nil || body != "Export ticket" || lookups.Load() != 2 {
					t.Fatalf("read %q, error %v, %d lookups", body, err, lookups.Load())
				}
			case tc.want != nil:
				if !errors.Is(err, tc.want) || body != "" {
					t.Fatalf("read %q, error %v; want %v", body, err, tc.want)
				}
			default:
				if err == nil || !strings.Contains(err.Error(), "public ticket redirect") {
					t.Fatalf("accepted an unsafe redirect: %v", err)
				}
			}
		})
	}
}

func TestPublicTicketUsesOriginalTLSIdentity(t *testing.T) {
	const host = "tickets.example.com" // The httptest certificate covers *.example.com.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host || r.TLS.ServerName != host {
			t.Errorf("HTTP host %s, TLS server %s, want %s", r.Host, r.TLS.ServerName, host)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "Export ticket")
	}))
	defer server.Close()

	dialer := net.Dialer{}
	client := publicTicketClient(func(_ context.Context, _, _ string) ([]netip.Addr, error) {
		return []netip.Addr{publicFixtureIP}, nil
	}, func(ctx context.Context, network, target string) (net.Conn, error) {
		if target != "93.184.216.34:443" {
			return nil, errors.New("HTTPS dial was not pinned to the validated address")
		}
		return dialer.DialContext(ctx, network, server.Listener.Addr().String())
	})
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	if body, err := readPublicTicket(t.Context(), "https://"+host+"/ticket", client); err != nil || body != "Export ticket" {
		t.Fatalf("HTTPS read %q: %v", body, err)
	}
	if _, err := readPublicTicket(t.Context(), "https://tickets.example/ticket", client); err == nil {
		t.Fatal("accepted a certificate for the wrong hostname")
	}
}
