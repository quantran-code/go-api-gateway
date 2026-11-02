# go-api-gateway

Single entry point for the library API. Routes `/api/v1/members`, `/api/v1/books`
and `/api/v1/loans` to the owning service and propagates `X-Request-Id` end to end.

```sh
MEMBER_SERVICE_URL=http://localhost:8081 \
CATALOG_SERVICE_URL=http://localhost:8082 \
LOAN_SERVICE_URL=http://localhost:8083 \
go run .
```

* `GET /health` — the gateway itself is up.
* A service that cannot be reached is answered `502 {"error":"upstream unavailable"}`.

Run the tests with `go test ./...`.
