package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// apiPrefix is the public prefix the gateway strips before it forwards a request
// to the service that owns the resource.
const apiPrefix = "/api"

// NewServiceProxy returns the handler that forwards requests to target with the
// /api prefix stripped. Everything else — method, query, headers and body —
// reaches the service as the client sent it.
func NewServiceProxy(target *url.URL, timeout time.Duration) http.Handler {
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			req.URL.Path = joinServicePath(target.Path, stripAPIPrefix(req.URL.Path))
			if req.URL.RawPath != "" {
				req.URL.RawPath = joinServicePath(target.Path, stripAPIPrefix(req.URL.RawPath))
			}
		},
		Transport: newUpstreamTransport(timeout),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeBadGateway(w, err)
		},
	}
}

// newUpstreamTransport bounds how long the gateway waits for the service to
// start answering, so an unresponsive service cannot hold a client forever.
func newUpstreamTransport(timeout time.Duration) *http.Transport {
	return &http.Transport{
		ResponseHeaderTimeout: timeout,
	}
}

// stripAPIPrefix removes the leading apiPrefix from path. A path that is exactly
// the prefix becomes "/" so the rewritten request never carries an empty path,
// and a path that does not start with the prefix is returned unchanged.
func stripAPIPrefix(path string) string {
	if path == apiPrefix {
		return "/"
	}
	if !strings.HasPrefix(path, apiPrefix+"/") {
		return path
	}

	trimmed := path[len(apiPrefix):]
	rest := trimmed[1:]
	// A path that names only the resource itself ("api/members") is the
	// service root; a sub-path keeps the segments after the api prefix.
	if rest != "" && !strings.Contains(rest, "/") {
		return "/"
	}

	return trimmed
}

// joinServicePath puts the stripped request path under whatever path the service
// URL itself carries, which is usually nothing.
func joinServicePath(base, path string) string {
	if base == "" {
		return path
	}
	return strings.TrimSuffix(base, "/") + path
}

// badGatewayResponse is the body returned when a service cannot be reached.
type badGatewayResponse struct {
	Error string `json:"error"`
}

// writeBadGateway logs the transport error and answers the client with 502 and a
// JSON body.
func writeBadGateway(w http.ResponseWriter, err error) {
	log.Printf("upstream request failed: %v", err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(badGatewayResponse{Error: "upstream unavailable"})
}
