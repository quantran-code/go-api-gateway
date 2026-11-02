// Command api-gateway is the single entry point for the library API: it routes
// /api/v1/members, /api/v1/books and /api/v1/loans to the service that owns each
// resource, and stamps every request with a correlation id.
package main

import (
	"log"
	"net/http"
)

func main() {
	cfg, err := ConfigFromEnv()
	if err != nil {
		log.Fatalf("configuration: %v", err)
	}
	log.Printf("api gateway listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, NewRouter(cfg)); err != nil {
		log.Fatal(err)
	}
}
