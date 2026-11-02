# go-api-gateway

The address clients use for the library API.

```sh
PORT=8080 go run .
```

| Route | Behaviour |
|---|---|
| `GET /health` | `{"status":"ok"}` when the process is up |

Members, books and loans are owned by their own services; the gateway is the only
address a client needs. Run the tests with `go test ./...`.
