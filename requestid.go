package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"strconv"
	"time"
)

// RequestIDHeader is the header that carries the identifier correlating one
// client request with the service that answers it.
const RequestIDHeader = "X-Request-ID"

// requestIDKey is the request context key the middleware stores the identifier
// under, so handlers can read it without depending on the header.
type requestIDKey struct{}

// newRequestID returns a fresh identifier: 16 bytes from crypto/rand, hex
// encoded to 32 lowercase hex characters. It never returns an empty string.
func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// rand.Read does not fail in practice; a time-based value still keeps
		// the request correlated instead of leaving the header empty.
		log.Printf("generating request id: %v", err)
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}

	return hex.EncodeToString(buf[:])
}

// RequestIDMiddleware tags every request with an X-Request-ID. An identifier the
// caller already sent is reused unchanged; otherwise one is minted here. The
// same value is set on the forwarded request, on the response and on the request
// context, and one log line records it.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = newRequestID()
		}

		// The proxy clones the inbound headers, so setting the header here is
		// what carries the identifier on to the service.
		r.Header.Set(RequestIDHeader, id)

		// The header goes on the writer before next runs, so responses the
		// gateway answers itself (the router's 404, the proxy's 502) carry it.
		w.Header().Set(RequestIDHeader, id)

		log.Printf("request_id=%s method=%s path=%s", id, r.Method, r.URL.Path)

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// RequestIDFromContext returns the identifier RequestIDMiddleware stored on ctx,
// or "" when the middleware did not run.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)

	return id
}
