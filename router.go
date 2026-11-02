package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Config is everything the gateway needs: where it listens and where the three
// services live. Every URL is required — a gateway that silently drops a route
// is worse than one that refuses to start.
type Config struct {
	Port       string
	MemberURL  string
	CatalogURL string
	LoanURL    string
}

// ConfigFromEnv reads PORT and the three service URLs. PORT defaults to 8080.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Port:       envOr("PORT", "8080"),
		MemberURL:  strings.TrimRight(os.Getenv("MEMBER_SERVICE_URL"), "/"),
		CatalogURL: strings.TrimRight(os.Getenv("CATALOG_SERVICE_URL"), "/"),
		LoanURL:    strings.TrimRight(os.Getenv("LOAN_SERVICE_URL"), "/"),
	}
	for _, entry := range []struct{ name, value string }{
		{"MEMBER_SERVICE_URL", cfg.MemberURL},
		{"CATALOG_SERVICE_URL", cfg.CatalogURL},
		{"LOAN_SERVICE_URL", cfg.LoanURL},
	} {
		if entry.value == "" {
			return Config{}, fmt.Errorf("%s is required", entry.name)
		}
		if _, err := url.Parse(entry.value); err != nil {
			return Config{}, fmt.Errorf("%s: %w", entry.name, err)
		}
	}
	return cfg, nil
}

// NewRouter wires the public API. chi's RequestID middleware sets X-Request-Id
// when the caller did not, and leaves an incoming value alone, which is what lets
// one identifier follow a request across all four services.
func NewRouter(cfg Config) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(requestLog)

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Handle("/members", forward(cfg.MemberURL, "/members"))
		r.Handle("/members/*", forward(cfg.MemberURL, "/members"))
		r.Handle("/books", forward(cfg.CatalogURL, "/books"))
		r.Handle("/books/*", forward(cfg.CatalogURL, "/books"))
		r.Handle("/loans", forward(cfg.LoanURL, "/loans"))
		r.Handle("/loans/*", forward(cfg.LoanURL, "/loans"))
	})
	return r
}

// forward proxies /api/v1<public> to <target><suffix>, keeping the rest of the
// path intact so the service sees its own routes unchanged.
func forward(target, prefix string) http.Handler {
	u, err := url.Parse(target)
	if err != nil {
		panic("invalid service url: " + target)
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		rest := strings.TrimPrefix(req.URL.Path, "/api/v1")
		req.URL.Path = prefix + strings.TrimPrefix(rest, prefix)
		req.Host = u.Host
		if id := middleware.GetReqID(req.Context()); id != "" {
			req.Header.Set("X-Request-Id", id)
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		writeError(w, http.StatusBadGateway, "upstream unavailable")
	}
	return proxy
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mirror the id back to the caller before the status line goes out: a client
		// that reports a failure needs the same identifier the services logged.
		if id := middleware.GetReqID(r.Context()); id != "" {
			w.Header().Set("X-Request-Id", id)
		}
		started := time.Now()
		next.ServeHTTP(w, r)
		// One line per request, carrying the correlation id so a failure can be
		// traced into the service logs without guessing.
		println(r.Method, r.URL.Path, middleware.GetReqID(r.Context()), time.Since(started).String())
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
