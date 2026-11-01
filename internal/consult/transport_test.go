package consult

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Local fixtures use plain HTTP, so this is the only test that sees ALPN. The
// advisers negotiate h2 whenever a client offers it.
func TestTransportNeverOffersHTTP2OverTLS(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.ProtoMajor != 1 {
			t.Errorf("request used %s", request.Proto)
		}
		_, _ = writer.Write([]byte(`{}`))
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	selected := newTransport()
	base := selected.client.Transport.(*http.Transport)
	config := &tls.Config{}
	if base.TLSClientConfig != nil {
		config = base.TLSClientConfig.Clone()
	}
	config.RootCAs = roots
	base.TLSClientConfig = config
	selected.endpoints = map[string]string{jevProvider: server.URL}
	raw, failure := selected.exchange(t.Context(), jevProvider, "fixture", []byte(`{}`))
	if failure != nil || raw == nil {
		t.Fatalf("exchange failed over TLS: %v", failure)
	}
}
