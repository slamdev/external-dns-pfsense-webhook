package integration

import (
	"context"
	"fmt"
	"net/http"

	"github.com/felixge/httpsnoop"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
	"github.com/oapi-codegen/runtime/strictmiddleware/nethttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func OpenapiValidationMiddleware(swagger *openapi3.T) func(next http.Handler) http.Handler {
	options := &nethttpmiddleware.Options{
		DoNotValidateServers:  true,
		SilenceServersWarning: true,
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, r *http.Request, opts nethttpmiddleware.ErrorHandlerOpts) {
			switch opts.StatusCode {
			case http.StatusNotFound:
				HandleHTTPNotFound(w, r)
			case http.StatusBadRequest:
				HandleHTTPBadRequest(w, r, err)
			default:
				HandleHTTPServerError(w, r, err)
			}
		},
		Options: openapi3filter.Options{
			ExcludeReadOnlyValidations: true,
			AuthenticationFunc:         openapi3filter.NoopAuthenticationFunc,
		},
	}
	openapi3filter.RegisterBodyDecoder("application/external.dns.webhook+json", openapi3filter.JSONBodyDecoder)
	return nethttpmiddleware.OapiRequestValidatorWithOptions(swagger, options)
}

func TelemetryStrictMiddleware(f nethttp.StrictHTTPHandlerFunc, operationID string) nethttp.StrictHTTPHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		labelServerRequest(ctx, operationID)
		return f(ctx, w, r, request)
	}
}

func TelemetryGlobalMiddleware(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "server", otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents))
}

func AccessLogsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := httpsnoop.CaptureMetrics(next, w, r)
		logHTTPRequest(r, m)
	})
}

func RecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				err := fmt.Errorf("%+v", err)
				HandleHTTPServerError(w, r, err)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type RequestURIKey struct{}

func RequestURIMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, RequestURIKey{}, r.RequestURI)
		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}
