package main

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// captureLog redirects the standard logger into a buffer for the duration of the
// test and returns that buffer.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(previous) })

	return &buf
}

// requestIDLogLines returns the logged lines that carry a request identifier.
func requestIDLogLines(logged string) []string {
	var lines []string
	for _, line := range strings.Split(logged, "\n") {
		if strings.Contains(line, "request_id=") {
			lines = append(lines, line)
		}
	}

	return lines
}

// newRecordingGateway builds a router whose three services all point at one fake
// upstream, which reports the X-Request-ID it received through seen.
func newRecordingGateway(t *testing.T, seen chan<- string) http.Handler {
	t.Helper()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get(RequestIDHeader)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "member")
	}))
	t.Cleanup(upstream.Close)

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parsing upstream URL: %v", err)
	}

	return NewRouter(Config{
		MembersURL:      target,
		BooksURL:        target,
		LoansURL:        target,
		UpstreamTimeout: time.Second,
	})
}

func TestGatewayForwardsAndEchoesRequestID(t *testing.T) {
	logs := captureLog(t)
	seen := make(chan string, 1)
	router := newRecordingGateway(t, seen)

	t.Run("mints an id when the caller sends none", func(t *testing.T) {
		logs.Reset()

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/members/7", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		forwarded := <-seen
		if forwarded == "" {
			t.Fatal("the upstream received no X-Request-ID")
		}

		if echoed := rec.Header().Get(RequestIDHeader); echoed != forwarded {
			t.Fatalf("response X-Request-ID = %q, want the forwarded value %q", echoed, forwarded)
		}

		lines := requestIDLogLines(logs.String())
		if len(lines) != 1 {
			t.Fatalf("logged %d lines carrying a request id, want exactly 1:\n%s", len(lines), logs.String())
		}

		wants := []string{
			"request_id=" + forwarded,
			"method=" + http.MethodGet,
			"path=/api/members/7",
		}
		for _, want := range wants {
			if !strings.Contains(lines[0], want) {
				t.Fatalf("log line %q does not contain %q", lines[0], want)
			}
		}
	})

	t.Run("reuses the id the caller sent", func(t *testing.T) {
		logs.Reset()

		req := httptest.NewRequest(http.MethodGet, "/api/members/7", nil)
		req.Header.Set(RequestIDHeader, "abc")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if forwarded := <-seen; forwarded != "abc" {
			t.Fatalf("upstream X-Request-ID = %q, want %q", forwarded, "abc")
		}
		if echoed := rec.Header().Get(RequestIDHeader); echoed != "abc" {
			t.Fatalf("response X-Request-ID = %q, want %q", echoed, "abc")
		}

		lines := requestIDLogLines(logs.String())
		if len(lines) != 1 || !strings.Contains(lines[0], "request_id=abc") {
			t.Fatalf("unexpected log output:\n%s", logs.String())
		}
	})

	t.Run("an unrouted path is tagged too", func(t *testing.T) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/authors", nil))

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		if id := rec.Header().Get(RequestIDHeader); id == "" {
			t.Fatal("the 404 response carries no X-Request-ID")
		}

		select {
		case got := <-seen:
			t.Fatalf("an upstream received a request carrying %q", got)
		default:
		}
	})
}

func TestRequestIDIsUniquePerRequest(t *testing.T) {
	captureLog(t)

	router := NewRouter(testConfig(t))

	ids := make(map[string]struct{}, 2)
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		id := rec.Header().Get(RequestIDHeader)
		if id == "" {
			t.Fatalf("request %d carries no X-Request-ID", i+1)
		}
		ids[id] = struct{}{}
	}

	if len(ids) != 2 {
		t.Fatalf("two requests produced %d distinct ids: %v", len(ids), ids)
	}
}
