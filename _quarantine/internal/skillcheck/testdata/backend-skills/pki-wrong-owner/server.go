package server

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
)

func TLSConfig(roots *x509.CertPool) *tls.Config {
	return &tls.Config{ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}
}

type ResourceStore interface {
	Read(string, string) ([]byte, error)
}

func ReadResource(store ResourceStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		value, err := store.Read(r.PathValue("owner"), r.PathValue("resource"))
		if err != nil {
			http.Error(w, "read failed", http.StatusInternalServerError)
			return
		}
		w.Write(value)
	}
}
