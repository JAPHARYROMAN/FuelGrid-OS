package server

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// handleMetrics serves the registry as Prometheus exposition format.
// Mounted at /metrics (not /api/v1/metrics) so scrapers don't need a
// session token or knowledge of the API surface.
//
// Authorization-wise the endpoint is intentionally open in dev — gate
// it via network policy / ingress allowlist in production.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.metrics == nil {
		http.NotFound(w, r)
		return
	}
	promhttp.HandlerFor(s.metrics.Registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
}
