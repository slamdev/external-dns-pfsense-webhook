package integration

import (
	"context"
	"fmt"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oapi-codegen/runtime/strictmiddleware/nethttp"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type APIConfig struct {
	StrictMiddlewares        []nethttp.StrictHTTPMiddlewareFunc
	Middlewares              []func(http.Handler) http.Handler
	RequestErrorHandlerFunc  func(w http.ResponseWriter, r *http.Request, err error)
	ResponseErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)
	BaseURL                  string
	ErrorHandlerFunc         func(w http.ResponseWriter, r *http.Request, err error)
}

func CreateAPIConfig(swagger *openapi3.T) (APIConfig, error) {
	cfg := APIConfig{}

	cfg.Middlewares = []func(http.Handler) http.Handler{OpenapiValidationMiddleware(swagger)}
	cfg.StrictMiddlewares = []nethttp.StrictHTTPMiddlewareFunc{TelemetryStrictMiddleware}
	cfg.RequestErrorHandlerFunc = HandleHTTPBadRequest
	cfg.ResponseErrorHandlerFunc = HandleHTTPCommonError
	cfg.ErrorHandlerFunc = HandleHTTPBadRequest

	baseURL, err := swagger.Servers.BasePath()
	if err != nil {
		return cfg, fmt.Errorf("failed to get base path from swagger spec; %w", err)
	}
	if baseURL != "/" {
		// we should set the base URL only if it is not the default value
		// because openapi generator will generate the final path with double slashes (//)
		cfg.BaseURL = baseURL
	}

	return cfg, nil
}

func APIHandler(mux *http.ServeMux) http.Handler {
	return RequestURIMiddleware(TelemetryGlobalMiddleware(AccessLogsMiddleware(RecoverMiddleware(mux))))
}

func createAndRecordProblemDetail(ctx context.Context, status int, err error) ProblemDetailV1 {
	title := http.StatusText(status)
	span := trace.SpanFromContext(ctx)
	var traceID string
	if span.SpanContext().HasTraceID() {
		traceID = span.SpanContext().TraceID().String()
	}
	if err != nil {
		span.RecordError(err)
	}
	span.SetStatus(codes.Error, title)
	requestURI, _ := ctx.Value(RequestURIKey{}).(string)

	errText := title
	if err != nil {
		errText = fmt.Sprintf("%+v", err)
	}

	return ProblemDetailV1{
		Instance: requestURI,
		Status:   status,
		Title:    title,
		TraceID:  traceID,
		Type:     "about:blank",
		Detail:   errText,
	}
}

func NewAPIClientHTTPError(msg string, status int, body []byte) error {
	return fmt.Errorf("%s, status: %d, body: %s", msg, status, body)
}

type ProblemDetailV1 struct {
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
	Status   int    `json:"status"`
	Title    string `json:"title"`
	TraceID  string `json:"traceId"`
	Type     string `json:"type"`
}
