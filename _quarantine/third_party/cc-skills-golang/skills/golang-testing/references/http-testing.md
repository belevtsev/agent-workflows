# HTTP Handler Testing

Use `httptest` package for testing HTTP handlers without starting a server.

Use `httptest.NewRecorder` for direct handler behavior. On Go 1.27+, use `httptest.NewTestServer(t, handler)` when the test needs client, transport, TLS, redirect, or other end-to-end HTTP behavior; it uses an in-memory network by default and registers cleanup with the test.

## Basic Handler Test

```go
func TestCreateUserHandler(t *testing.T) {
    tests := []struct {
        name           string
        body           string
        expectedStatus int
    }{
        {
            name:           "valid request",
            body:           `{"name": "Alice", "email": "alice@example.com"}`,
            expectedStatus: http.StatusCreated,
        },
        {
            name:           "invalid JSON",
            body:           `invalid json`,
            expectedStatus: http.StatusBadRequest,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            is := assert.New(t)

            req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(tt.body))
            req.Header.Set("Content-Type", "application/json")

            w := httptest.NewRecorder()
            handler := http.HandlerFunc(CreateUserHandler)
            handler.ServeHTTP(w, req)

            is.Equal(tt.expectedStatus, w.Code)
        })
    }
}
```

## End-to-End HTTP Test _(Go 1.27+)_

```go
func TestHealthOverHTTP(t *testing.T) {
    server := httptest.NewTestServer(t, http.HandlerFunc(HealthHandler))

    resp, err := server.Client().Get("http://service.test/health")
    if err != nil {
        t.Fatal(err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
    }
}
```

Do not call `server.Close`; `NewTestServer` registers cleanup. Use `server.Client()` because it is configured for the in-memory network and routes requests to the test handler regardless of hostname.

## Query Parameters and Headers

```go
func TestListUsersHandler(t *testing.T) {
    tests := []struct {
        name           string
        query          string
        authHeader     string
        expectedStatus int
    }{
        {
            name:           "paginated results",
            query:          "?page=1&limit=10",
            authHeader:     "Bearer token123",
            expectedStatus: http.StatusOK,
        },
        {
            name:           "missing auth",
            query:          "?page=1",
            authHeader:     "",
            expectedStatus: http.StatusUnauthorized,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            is := assert.New(t)

            req := httptest.NewRequest(http.MethodGet, "/users"+tt.query, nil)
            if tt.authHeader != "" {
                req.Header.Set("Authorization", tt.authHeader)
            }

            w := httptest.NewRecorder()
            handler := AuthMiddleware(ListUsersHandler)
            handler.ServeHTTP(w, req)

            is.Equal(tt.expectedStatus, w.Code)
        })
    }
}
```
