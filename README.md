# go-api-gateway

The address clients use for the library API.

```sh
PORT=8080 go run .
```

| Route | Behaviour |
|---|---|
| `GET /health` | `{"status":"ok"}` when the process is up |
| `GET /api/members/{id}` | read one member; forwarded to the member service as `GET /members/{id}` |
| `POST /api/loans` | borrow a book, with a body such as `{"book_id":1,"member_id":7}`; forwarded to the loan service as `POST /loans` |

Members, books and loans are owned by their own services; the gateway is the only
address a client needs. Only the `/api` prefix is stripped, so the resource name
travels on with the request: `/api/loans` reaches the loan service as `/loans`,
`/api/members/7` reaches the member service as `/members/7`, and the bare `/api`
reaches the service as `/`. Run the tests with `go test ./...`.

## Correlation with `X-Request-ID`

Every request is correlated by the `X-Request-ID` header. A value the caller
already sends is reused unchanged; when the header is absent the gateway mints
one (16 random bytes as 32 lowercase hex characters). That value is forwarded
unchanged to the service that answers the request, echoed back on the response —
including on `404` and `502` responses — and written to the gateway's log:

```text
request_id=1f0a4c9d5e7b8a2c3d4e5f60718293a4 method=GET path=/api/members/7
```
