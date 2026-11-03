package main

import (
	"strings"
	"testing"
)

func TestLoadConfigRequiresEveryServiceURL(t *testing.T) {
	valid := map[string]string{
		"MEMBERS_URL": "http://members.test:8081",
		"BOOKS_URL":   "http://books.test:8082",
		"LOANS_URL":   "http://loans.test:8083",
	}

	for _, missing := range ServiceURLVariables {
		t.Run(missing, func(t *testing.T) {
			getenv := func(key string) string {
				if key == missing {
					return ""
				}
				return valid[key]
			}

			_, err := LoadConfig(getenv)
			if err == nil {
				t.Fatalf("LoadConfig returned no error with %s unset", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Fatalf("error does not name %s: %v", missing, err)
			}
		})
	}

	t.Run("no variables set", func(t *testing.T) {
		_, err := LoadConfig(func(string) string { return "" })
		if err == nil {
			t.Fatal("LoadConfig substituted a default instead of failing")
		}
		if !strings.Contains(err.Error(), "MEMBERS_URL") {
			t.Fatalf("error does not name the first missing variable: %v", err)
		}
	})
}

func TestLoadConfigRejectsUnparsableURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "host and port without a scheme", raw: "members:8081"},
		{name: "unsupported scheme", raw: "ftp://books.test:8082"},
		{name: "relative path only", raw: "/members"},
		{name: "scheme without a host", raw: "http://"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string {
				switch key {
				case "MEMBERS_URL":
					return "http://members.test:8081"
				case "BOOKS_URL":
					return tc.raw
				case "LOANS_URL":
					return "http://loans.test:8083"
				}
				return ""
			}

			_, err := LoadConfig(getenv)
			if err == nil {
				t.Fatalf("LoadConfig accepted BOOKS_URL=%q", tc.raw)
			}
			if !strings.Contains(err.Error(), "BOOKS_URL") {
				t.Fatalf("error does not name BOOKS_URL: %v", err)
			}
		})
	}
}

func TestLoadConfigParsesAllThreeServices(t *testing.T) {
	values := map[string]string{
		"MEMBERS_URL": "http://members.test:8081",
		"BOOKS_URL":   "https://books.test:8082",
		"LOANS_URL":   "http://loans.test:8083",
	}

	cfg, err := LoadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	cases := []struct {
		name string
		got  string
		want string
	}{
		{name: "members", got: cfg.MembersURL.String(), want: values["MEMBERS_URL"]},
		{name: "books", got: cfg.BooksURL.String(), want: values["BOOKS_URL"]},
		{name: "loans", got: cfg.LoansURL.String(), want: values["LOANS_URL"]},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("URL = %q, want %q", tc.got, tc.want)
			}
		})
	}

	if cfg.MembersURL.Host != "members.test:8081" {
		t.Fatalf("members host = %q", cfg.MembersURL.Host)
	}
	if cfg.BooksURL.Host != "books.test:8082" {
		t.Fatalf("books host = %q", cfg.BooksURL.Host)
	}
	if cfg.LoansURL.Host != "loans.test:8083" {
		t.Fatalf("loans host = %q", cfg.LoansURL.Host)
	}
	if cfg.UpstreamTimeout != DefaultUpstreamTimeout {
		t.Fatalf("upstream timeout = %v, want %v", cfg.UpstreamTimeout, DefaultUpstreamTimeout)
	}
}
