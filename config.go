package main

import (
	"fmt"
	"net/url"
	"time"
)

// DefaultUpstreamTimeout bounds how long the gateway waits for a service to
// start answering a proxied request.
const DefaultUpstreamTimeout = 30 * time.Second

// ServiceURLVariables names the environment variables that carry the base URL
// of each service the gateway forwards to.
var ServiceURLVariables = []string{"MEMBERS_URL", "BOOKS_URL", "LOANS_URL"}

// Config is what the gateway needs to know before it starts listening.
type Config struct {
	MembersURL      *url.URL
	BooksURL        *url.URL
	LoansURL        *url.URL
	UpstreamTimeout time.Duration
}

// LoadConfig reads the service base URLs through getenv. Every service URL is
// required, and there is no default to fall back on: an unset or unusable
// value fails start-up with a message naming that variable.
func LoadConfig(getenv func(string) string) (Config, error) {
	members, err := parseServiceURL("MEMBERS_URL", getenv("MEMBERS_URL"))
	if err != nil {
		return Config{}, err
	}

	books, err := parseServiceURL("BOOKS_URL", getenv("BOOKS_URL"))
	if err != nil {
		return Config{}, err
	}

	loans, err := parseServiceURL("LOANS_URL", getenv("LOANS_URL"))
	if err != nil {
		return Config{}, err
	}

	return Config{
		MembersURL:      members,
		BooksURL:        books,
		LoansURL:        loans,
		UpstreamTimeout: DefaultUpstreamTimeout,
	}, nil
}

// parseServiceURL checks that raw is an absolute http or https URL. url.Parse on
// its own accepts far too much — "members:8081" parses as an opaque URL — so the
// scheme and host are checked explicitly.
func parseServiceURL(name, raw string) (*url.URL, error) {
	if raw == "" {
		return nil, fmt.Errorf("%s is required", name)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is not a valid URL: %w", name, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%s must be an absolute http or https URL, got %q", name, raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%s must include a host, got %q", name, raw)
	}

	return u, nil
}
