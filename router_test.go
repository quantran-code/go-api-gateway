package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// upstream records what the service actually received.
type upstream struct {
	path, requestID string
	status          int
}

func newUpstream(t *testing.T, got *upstream) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.requestID = r.Header.Get("X-Request-Id")
		if got.status != 0 {
			w.WriteHeader(got.status)
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
}

func TestRoutesEachPrefixToItsServiceAndStripsThePublicPrefix(t *testing.T) {
	member, catalog, loan := &upstream{}, &upstream{}, &upstream{}
	memberSrv, catalogSrv, loanSrv := newUpstream(t, member), newUpstream(t, catalog), newUpstream(t, loan)
	defer memberSrv.Close()
	defer catalogSrv.Close()
	defer loanSrv.Close()

	router := NewRouter(Config{MemberURL: memberSrv.URL, CatalogURL: catalogSrv.URL, LoanURL: loanSrv.URL})

	for _, tc := range []struct {
		url  string
		want string
		got  *upstream
	}{
		{"/api/v1/members/7", "/members/7", member},
		{"/api/v1/books?page=2", "/books", catalog},
		{"/api/v1/loans", "/loans", loan},
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", tc.url, rec.Code)
		}
		if tc.got.path != tc.want {
			t.Fatalf("%s: service saw %q, want %q", tc.url, tc.got.path, tc.want)
		}
	}
}

func TestCorrelationIDIsGeneratedAndForwarded(t *testing.T) {
	got := &upstream{}
	srv := newUpstream(t, got)
	defer srv.Close()
	router := NewRouter(Config{MemberURL: srv.URL, CatalogURL: srv.URL, LoanURL: srv.URL})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/members", nil))
	if got.requestID == "" {
		t.Fatal("the service received no X-Request-Id")
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("the response carries no X-Request-Id")
	}
}

func TestAnIncomingCorrelationIDIsKept(t *testing.T) {
	got := &upstream{}
	srv := newUpstream(t, got)
	defer srv.Close()
	router := NewRouter(Config{MemberURL: srv.URL, CatalogURL: srv.URL, LoanURL: srv.URL})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/members", nil)
	req.Header.Set("X-Request-Id", "caller-chosen")
	router.ServeHTTP(httptest.NewRecorder(), req)

	if got.requestID != "caller-chosen" {
		t.Fatalf("correlation id was replaced: %q", got.requestID)
	}
}

func TestAnUnreachableServiceIs502NotAHang(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	dead := srv.URL
	srv.Close()

	router := NewRouter(Config{MemberURL: dead, CatalogURL: dead, LoanURL: dead})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/members", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "upstream unavailable") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestConfigRequiresEveryServiceURL(t *testing.T) {
	t.Setenv("MEMBER_SERVICE_URL", "http://members")
	t.Setenv("CATALOG_SERVICE_URL", "")
	t.Setenv("LOAN_SERVICE_URL", "http://loans")
	if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "CATALOG_SERVICE_URL") {
		t.Fatalf("expected a CATALOG_SERVICE_URL error, got %v", err)
	}
}
