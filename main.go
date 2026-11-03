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
// forwarded to the service that owns the resource.
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
		proxy := NewServiceProxy(resource.target, cfg.UpstreamTimeout)
		// The resource prefix is stripped before the proxy sees the request, so
		// only the part the service itself expects is forwarded.
		handler := http.StripPrefix(resource.prefix, proxy)
		// The two patterns cover the bare prefix and every sub-path under it.
		r.Handle(resource.prefix, handler)
		r.Handle(resource.prefix+"/*", handler)
	}

	return r
}
