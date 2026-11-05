package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// runGatewayMainEnv marks the re-executed test binary as a run of main().
const runGatewayMainEnv = "GO_API_GATEWAY_RUN_MAIN"

// testConfig builds a Config from three valid URLs, so the router can be wired
// without any service actually running.
func testConfig(t *testing.T) Config {
	t.Helper()

	values := map[string]string{
		"MEMBERS_URL": "http://members.test:8081",
		"BOOKS_URL":   "http://books.test:8082",
		"LOANS_URL":   "http://loans.test:8083",
	}

	cfg, err := LoadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("building config: %v", err)
	}

	return cfg
}

// recordingUpstream is a fake service that reports every request it receives.
type recordingUpstream struct {
	server *httptest.Server
	hits   chan string
}

func newRecordingUpstream(t *testing.T, name string) *recordingUpstream {
	t.Helper()

	upstream := &recordingUpstream{hits: make(chan string, 16)}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.hits <- name + " " + r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, name)
	}))
	t.Cleanup(upstream.server.Close)

	return upstream
}

// url is the parsed base URL of the fake service.
func (u *recordingUpstream) url(t *testing.T) *url.URL {
	t.Helper()

	parsed, err := url.Parse(u.server.URL)
	if err != nil {
		t.Fatalf("parsing upstream URL: %v", err)
	}

	return parsed
}

// drain returns the requests the fake service has received so far and forgets
// them.
func (u *recordingUpstream) drain() []string {
	var hits []string
	for {
		select {
		case hit := <-u.hits:
			hits = append(hits, hit)
		default:
			return hits
		}
	}
}

func TestHealthReportsTheGatewayIsUp(t *testing.T) {
	rec := httptest.NewRecorder()
	NewRouter(testConfig(t)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestRouterSendsEachPrefixToItsOwnService(t *testing.T) {
	members := newRecordingUpstream(t, "members")
	books := newRecordingUpstream(t, "books")
	loans := newRecordingUpstream(t, "loans")

	router := NewRouter(Config{
		MembersURL:      members.url(t),
		BooksURL:        books.url(t),
		LoansURL:        loans.url(t),
		UpstreamTimeout: time.Second,
	})

	cases := []struct {
		name        string
		resource    string
		method      string
		requestPath string
		body        string
		wantHit     string
	}{
		{name: "members sub path", resource: "members", requestPath: "/api/members/7", wantHit: "members /members/7"},
		{name: "members bare prefix", resource: "members", requestPath: "/api/members", wantHit: "members /members"},
		{name: "books sub path", resource: "books", requestPath: "/api/books/7", wantHit: "books /books/7"},
		{name: "books bare prefix", resource: "books", requestPath: "/api/books", wantHit: "books /books"},
		{name: "loans sub path", resource: "loans", requestPath: "/api/loans/7", wantHit: "loans /loans/7"},
		{name: "loans bare prefix", resource: "loans", requestPath: "/api/loans", wantHit: "loans /loans"},
		{
			name:        "borrow a book goes to the loans service only",
			resource:    "loans",
			method:      http.MethodPost,
			requestPath: "/api/loans",
			body:        `{"book_id":1,"member_id":7}`,
			wantHit:     "loans /loans",
		},
		{
			name:        "return a loan goes to the loans service only",
			resource:    "loans",
			method:      http.MethodPost,
			requestPath: "/api/loans/1/return",
			wantHit:     "loans /loans/1/return",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			var body io.Reader
			if tc.body != "" {
				body = strings.NewReader(tc.body)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(method, tc.requestPath, body))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, http.StatusOK, rec.Body.String())
			}
			if got := rec.Body.String(); got != tc.resource {
				t.Fatalf("body = %q, want the %s service to answer", got, tc.resource)
			}

			services := []struct {
				resource string
				upstream *recordingUpstream
			}{
				{resource: "members", upstream: members},
				{resource: "books", upstream: books},
				{resource: "loans", upstream: loans},
			}
			for _, service := range services {
				hits := service.upstream.drain()
				if service.resource != tc.resource {
					if len(hits) != 0 {
						t.Fatalf("%s service received %v, want nothing", service.resource, hits)
					}
					continue
				}
				if len(hits) != 1 {
					t.Fatalf("%s service received %v, want exactly one request", service.resource, hits)
				}
				if hits[0] != tc.wantHit {
					t.Fatalf("%s service received %q, want %q", service.resource, hits[0], tc.wantHit)
				}
			}
		})
	}
}

func TestUnknownPrefixIsNotForwarded(t *testing.T) {
	members := newRecordingUpstream(t, "members")
	books := newRecordingUpstream(t, "books")
	loans := newRecordingUpstream(t, "loans")

	router := NewRouter(Config{
		MembersURL:      members.url(t),
		BooksURL:        books.url(t),
		LoansURL:        loans.url(t),
		UpstreamTimeout: time.Second,
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/authors", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	for _, upstream := range []*recordingUpstream{members, books, loans} {
		if hits := upstream.drain(); len(hits) != 0 {
			t.Fatalf("an upstream received %v for an unknown prefix", hits)
		}
	}
}

// TestRunGatewayMain is not a test of its own: it is the entry point the
// subprocess test re-executes so main() can be exercised as a real process.
func TestRunGatewayMain(t *testing.T) {
	if os.Getenv(runGatewayMainEnv) != "1" {
		return
	}

	main()
}

// gatewayEnv builds the environment for a subprocess run of main(): the service
// URL variables and PORT are dropped from the inherited environment and replaced
// with the values the case supplies.
func gatewayEnv(values map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(values))

	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "PORT" {
			continue
		}
		skip := false
		for _, name := range ServiceURLVariables {
			if key == name {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		env = append(env, entry)
	}

	for key, value := range values {
		env = append(env, key+"="+value)
	}

	return env
}

func TestMainExitsNonZeroWhenServiceURLIsMissing(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "MEMBERS_URL unset",
			env: map[string]string{
				"BOOKS_URL": "http://books.test:8082",
				"LOANS_URL": "http://loans.test:8083",
			},
			want: "MEMBERS_URL",
		},
		{
			name: "LOANS_URL is not a URL",
			env: map[string]string{
				"MEMBERS_URL": "http://members.test:8081",
				"BOOKS_URL":   "http://books.test:8082",
				"LOANS_URL":   "not-a-url",
			},
			want: "LOANS_URL",
		},
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestRunGatewayMain$")
			cmd.Env = append(gatewayEnv(tc.env), runGatewayMainEnv+"=1")

			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("the gateway started with %s, want a non-zero exit:\n%s", tc.want, out)
			}

			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("running the gateway: %v\n%s", err, out)
			}
			if exitErr.ExitCode() == 0 {
				t.Fatalf("exit status = 0, want non-zero:\n%s", out)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Fatalf("output does not name %s:\n%s", tc.want, out)
			}
		})
	}
}
