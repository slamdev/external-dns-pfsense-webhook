package integration

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/hashicorp/go-cleanhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type HTTPServer interface {
	Start() error
	Stop(ctx context.Context) error
}

type httpServer struct {
	srv *http.Server
}

func NewHTTPServer(port int32, handler http.Handler) HTTPServer {
	srv := http.Server{
		Addr: fmt.Sprintf(":%d", port), Handler: handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	return &httpServer{srv: &srv}
}

func (h *httpServer) Start() (err error) {
	slog.Info("starting http server", "addr", h.srv.Addr)
	if err = h.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to start http server: %w", err)
	}
	return nil
}

func (h *httpServer) Stop(ctx context.Context) error {
	if err := h.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown http server: %w", err)
	}
	return nil
}

func HandleHTTPBadRequest(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusBadRequest
	p := createAndRecordProblemDetail(r.Context(), status, err)
	writeProblem(w, r, p)
}

func HandleHTTPNotFound(w http.ResponseWriter, r *http.Request) {
	HandleHTTPNotFoundWithError(w, r, nil)
}

func HandleHTTPNotFoundWithError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusNotFound
	p := createAndRecordProblemDetail(r.Context(), status, err)
	writeProblem(w, r, p)
}

func HandleHTTPForbidden(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusForbidden
	p := createAndRecordProblemDetail(r.Context(), status, err)
	writeProblem(w, r, p)
}

func HandleHTTPUnauthorized(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusUnauthorized
	p := createAndRecordProblemDetail(r.Context(), status, err)
	writeProblem(w, r, p)
}

func HandleHTTPConflict(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusConflict
	p := createAndRecordProblemDetail(r.Context(), status, err)
	writeProblem(w, r, p)
}

func HandleHTTPUnprocessableEntity(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusUnprocessableEntity
	p := createAndRecordProblemDetail(r.Context(), status, err)
	writeProblem(w, r, p)
}

func HandleHTTPServerError(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "unexpected error occurred", "err", err, "stack", string(debug.Stack()))

	status := http.StatusInternalServerError
	p := createAndRecordProblemDetail(r.Context(), status, err)
	writeProblem(w, r, p)
}

func HandleHTTPCommonError(w http.ResponseWriter, r *http.Request, err error) {
	if IsValidationError(err) {
		HandleHTTPBadRequest(w, r, err)
		return
	}
	if IsMissingEntityError(err) {
		HandleHTTPNotFoundWithError(w, r, err)
		return
	}
	if IsResourceConflictError(err) {
		HandleHTTPConflict(w, r, err)
		return
	}
	if IsAccessDeniedError(err) {
		HandleHTTPForbidden(w, r, err)
		return
	}
	HandleHTTPServerError(w, r, err)
}

func writeProblem(w http.ResponseWriter, r *http.Request, p ProblemDetailV1) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		slog.ErrorContext(r.Context(), "failed to write problem to response", "err", err)
	}
}

func NewHTTPClient(serverName string, middlewares ...HTTPClientMiddleware) *http.Client {
	return NewHTTPClientWithTLS(serverName, nil, middlewares...)
}

func NewHTTPClientWithTLS(serverName string, tlsCfg *tls.Config, middlewares ...HTTPClientMiddleware) *http.Client {
	pooledTransport := cleanhttp.DefaultPooledTransport()
	pooledTransport.TLSClientConfig = tlsCfg
	var transport http.RoundTripper = otelhttp.NewTransport(
		pooledTransport,
		otelhttp.WithServerName(serverName),
		otelhttp.WithMetricAttributesFn(func(r *http.Request) []attribute.KeyValue {
			return labelClientRequest(serverName, r)
		}),
	)
	for _, middleware := range middlewares {
		transport = &middlewareRoundTripper{
			roundTripper: transport,
			middleware:   middleware,
		}
	}
	return &http.Client{Timeout: time.Minute, Transport: transport}
}

type HTTPClientMiddleware func(req *http.Request, next http.RoundTripper) (*http.Response, error)

type middlewareRoundTripper struct {
	roundTripper http.RoundTripper
	middleware   func(req *http.Request, next http.RoundTripper) (*http.Response, error)
}

func (t *middlewareRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.middleware(req, t.roundTripper)
}
