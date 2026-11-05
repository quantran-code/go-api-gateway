package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// upstreamRequest is what the fake service saw when the gateway forwarded to it.
type upstreamRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
}

// newProxyFor starts a fake service and returns a proxy that forwards to it.
func newProxyFor(t *testing.T, timeout time.Duration, handler http.HandlerFunc) (http.Handler, *httptest.Server) {
	t.Helper()

	upstream := httptest.NewServer(handler)
	target, err := url.Parse(upstream.URL)
	if err != nil {
		upstream.Close()
		t.Fatalf("parsing upstream URL: %v", err)
	}

	return NewServiceProxy(target, timeout), upstream
}

// assertGatewayError checks a 502 JSON body and returns the error message it carries.
func assertGatewayError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type = %q, want application/json", ct)
	}

	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if payload.Error == "" {
		t.Fatalf("error field is empty: %s", rec.Body.String())
	}

	return payload.Error
}

func TestProxyStripsAPIPrefixAndForwardsSubPaths(t *testing.T) {
	cases := []struct {
		name      string
		method    string
		target    string
		body      string
		wantPath  string
		wantQuery string
	}{
		{
			name:      "sub path keeps what follows the api prefix",
			method:    http.MethodPost,
			target:    "/api/members/42/loans?active=true",
			body:      "x",
			wantPath:  "/members/42/loans",
			wantQuery: "active=true",
		},
		{
			name:     "bare prefix keeps the resource name",
			method:   http.MethodGet,
			target:   "/api/members",
			wantPath: "/members",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan upstreamRequest, 1)
			proxy, upstream := newProxyFor(t, time.Second, func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("reading forwarded body: %v", err)
				}
				seen <- upstreamRequest{
					Method: r.Method,
					Path:   r.URL.Path,
					Query:  r.URL.RawQuery,
					Body:   string(body),
				}
				w.WriteHeader(http.StatusOK)
			})
			defer upstream.Close()

			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body)))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}

			got := <-seen
			if got.Path != tc.wantPath {
				t.Fatalf("upstream path = %q, want %q", got.Path, tc.wantPath)
			}
			if got.Query != tc.wantQuery {
				t.Fatalf("upstream query = %q, want %q", got.Query, tc.wantQuery)
			}
			if got.Method != tc.method {
				t.Fatalf("upstream method = %q, want %q", got.Method, tc.method)
			}
			if got.Body != tc.body {
				t.Fatalf("upstream body = %q, want %q", got.Body, tc.body)
			}
		})
	}
}

func TestProxyForwardsEveryMethod(t *testing.T) {
	const responseBody = `{"status":"handled"}`

	methods := []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodHead,
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			seen := make(chan string, 1)
			proxy, upstream := newProxyFor(t, time.Second, func(w http.ResponseWriter, r *http.Request) {
				seen <- r.Method
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, responseBody)
			})
			defer upstream.Close()

			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, httptest.NewRequest(method, "/api/books/1", nil))

			if got := <-seen; got != method {
				t.Fatalf("upstream method = %q, want %q", got, method)
			}
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("content type = %q, want application/json", ct)
			}

			wantBody := responseBody
			if method == http.MethodHead {
				wantBody = ""
			}
			if got := rec.Body.String(); got != wantBody {
				t.Fatalf("body = %q, want %q", got, wantBody)
			}
		})
	}
}

func TestProxyForwardsBorrowRequestToTheLoanService(t *testing.T) {
	const borrowBody = `{"book_id":1,"member_id":7}`

	seen := make(chan upstreamRequest, 1)
	proxy, upstream := newProxyFor(t, time.Second, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading forwarded body: %v", err)
		}
		seen <- upstreamRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Body:   string(body),
		}
		w.WriteHeader(http.StatusCreated)
	})
	defer upstream.Close()

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/loans", strings.NewReader(borrowBody)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusCreated, rec.Body.String())
	}

	got := <-seen
	if got.Method != http.MethodPost {
		t.Fatalf("upstream method = %q, want %q", got.Method, http.MethodPost)
	}
	if got.Path != "/loans" {
		t.Fatalf("upstream path = %q, want %q", got.Path, "/loans")
	}
	if got.Query != "" {
		t.Fatalf("upstream query = %q, want none", got.Query)
	}
	if got.Body != borrowBody {
		t.Fatalf("upstream body = %q, want %q", got.Body, borrowBody)
	}
}

func TestProxyForwardsReturnRequestToTheLoanService(t *testing.T) {
	const returnedLoan = `{"id":1,"book_id":1,"member_id":7,"returned_at":"2024-05-01T10:00:00Z"}`

	seen := make(chan upstreamRequest, 1)
	proxy, upstream := newProxyFor(t, time.Second, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading forwarded body: %v", err)
		}
		seen <- upstreamRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Body:   string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, returnedLoan)
	})
	defer upstream.Close()

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/loans/1/return", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != returnedLoan {
		t.Fatalf("body = %q, want the loan service's body %q", got, returnedLoan)
	}

	got := <-seen
	if got.Method != http.MethodPost {
		t.Fatalf("upstream method = %q, want %q", got.Method, http.MethodPost)
	}
	if got.Path != "/loans/1/return" {
		t.Fatalf("upstream path = %q, want %q", got.Path, "/loans/1/return")
	}
	if got.Query != "" {
		t.Fatalf("upstream query = %q, want none", got.Query)
	}
	if got.Body != "" {
		t.Fatalf("upstream body = %q, want none", got.Body)
	}
}

func TestProxyForwardsTheBareAPIPrefixAsTheServiceRoot(t *testing.T) {
	seen := make(chan string, 1)
	proxy, upstream := newProxyFor(t, time.Second, func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	defer upstream.Close()

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if got := <-seen; got != "/" {
		t.Fatalf("upstream path = %q, want %q", got, "/")
	}
}

func TestProxyReturns502JSONWhenUpstreamUnreachable(t *testing.T) {
	seen := make(chan struct{}, 1)
	proxy, upstream := newProxyFor(t, time.Second, func(w http.ResponseWriter, _ *http.Request) {
		seen <- struct{}{}
	})
	upstream.Close() // nothing is listening on that address any more

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/books", nil))

	assertGatewayError(t, rec)

	select {
	case <-seen:
		t.Fatal("the unreachable upstream received a request")
	default:
	}
}

func TestProxyReturns502JSONWhenUpstreamTimesOut(t *testing.T) {
	release := make(chan struct{})
	proxy, upstream := newProxyFor(t, 100*time.Millisecond, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() {
		close(release)
		upstream.Close()
	})

	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/loans", nil))

	assertGatewayError(t, rec)
}
