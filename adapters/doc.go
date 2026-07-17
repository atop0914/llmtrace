// Package adapters provides HTTP middleware for integrating llmtrace
// into Go web applications.
//
// The middleware is built on net/http and works with any framework that
// supports http.Handler, including Gin, Echo, Chi, and stdlib ServeMux.
//
// # Features
//
//   - Automatic request ID generation and propagation (X-Request-ID)
//   - OpenTelemetry span creation per HTTP request
//   - Response headers with timing and token usage metadata
//   - Panic recovery with structured error responses
//   - Context-based request metadata (provider, model, tokens)
//
// # Usage with net/http (stdlib)
//
//	mux := http.NewServeMux()
//	middleware := adapters.Middleware(adapters.DefaultConfig())
//
//	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//	    data := adapters.RequestDataFromContext(r.Context())
//	    data.Provider = "openai"
//	    data.TokensUsed = 150
//	    w.Write([]byte(`{"response":"hello"}`))
//	})
//	mux.Handle("POST /v1/chat", middleware(handler))
//
// # Usage with Gin
//
//	r := gin.Default()
//	// Use echo.WrapMiddleware(adapters.Middleware(cfg)) pattern:
//	r.Use(func(c *gin.Context) {
//	    adapters.Middleware(adapters.DefaultConfig())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//	        c.Request = r
//	        c.Next()
//	    })).ServeHTTP(c.Writer, c.Request)
//	})
//
// # Usage with Echo
//
//	e := echo.New()
//	e.Use(echo.WrapMiddleware(adapters.Middleware(adapters.DefaultConfig())))
//
// # Usage with Chi
//
//	r := chi.NewRouter()
//	r.Use(adapters.Middleware(adapters.DefaultConfig()))
//
// # Response Headers
//
// The middleware adds these headers to responses (when enabled):
//
//   - X-Request-ID: unique correlation ID for request tracing
//   - X-Response-Time-Ms: request processing duration in milliseconds
//   - X-LLM-Provider: LLM provider name (set via SetProvider)
//   - X-Tokens-Used: total tokens consumed (set via SetTokensUsed)
package adapters
