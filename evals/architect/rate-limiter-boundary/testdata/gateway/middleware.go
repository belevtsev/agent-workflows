package gateway

import (
	"net/http"
	"strings"
)

// tenantFromRequest returns the tenant named by the X-Tenant header, or "".
func tenantFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Tenant"))
}

// Authenticate rejects requests without a known tenant before the handler runs.
func Authenticate(cfg *Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant := tenantFromRequest(r)
		if _, ok := cfg.Tenants[tenant]; !ok {
			http.Error(w, "unknown tenant", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
