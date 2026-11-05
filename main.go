// Command api-gateway is the address clients use for the library API.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/go-chi/chi/v5"
)

func main() {
	cfg, err := LoadConfig(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("api gateway listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, NewRouter(cfg)))
}

// NewRouter wires the routes the gateway answers itself. Everything else is
// forwarded to the service that owns the resource. Every request is tagged with
// an X-Request-ID that travels on to the service.
func NewRouter(cfg Config) http.Handler {
	r := chi.NewRouter()
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	resources := []struct {
		prefix string
		target *url.URL
	}{
		{prefix: apiPrefix + "/members", target: cfg.MembersURL},
		{prefix: apiPrefix + "/books", target: cfg.BooksURL},
		{prefix: apiPrefix + "/loans", target: cfg.LoansURL},
	}

	for _, resource := range resources {
		// Only the /api prefix is stripped, by the proxy's own Director, so the
		// resource name is preserved: /api/loans reaches the loan service as
		// /loans, and /api/members/7 arrives as /members/7, which is the route
		// the service registers.
		proxy := NewServiceProxy(resource.target, cfg.UpstreamTimeout)
		// The two patterns cover the bare prefix and every sub-path under it.
		r.Handle(resource.prefix, proxy)
		r.Handle(resource.prefix+"/*", proxy)
	}

	// The middleware wraps the whole router, so the responses the gateway
	// answers itself are tagged as well as the proxied ones.
	return RequestIDMiddleware(r)
}
