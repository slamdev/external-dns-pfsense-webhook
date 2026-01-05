package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alexliesenfeld/health"
	"github.com/felixge/httpsnoop"
	"github.com/go-logr/logr"
	"github.com/lmittmann/tint"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	slogotel "github.com/remychantenay/slog-otel"
	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/contrib/propagators/autoprop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.18.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func CreateTelemetryResource(_ context.Context) *resource.Resource {
	res := resource.Default()
	otel.SetTextMapPropagator(autoprop.NewTextMapPropagator())
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Error("otel error", "err", err)
	}))
	return res
}

func ConfigureTraceProvider(ctx context.Context, res *resource.Resource, output string) (*trace.TracerProvider, error) {
	var exporter trace.SpanExporter
	if output == "remote" {
		var err error
		if exporter, err = otlptracegrpc.New(ctx); err != nil {
			return nil, fmt.Errorf("failed to create trace exporter: %w", err)
		}
	} else {
		var writer io.Writer
		if output == "noop" {
			writer = NoopWriter{}
		} else {
			writer = &SlogWriter{Log: slog.Default(), Level: slog.LevelDebug}
		}
		var err error
		if exporter, err = stdouttrace.New(stdouttrace.WithWriter(writer)); err != nil {
			return nil, fmt.Errorf("failed to create trace exporter: %w", err)
		}
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	return tp, nil
}

func ConfigureMetricProvider(_ context.Context, res *resource.Resource, output string) (*metric.MeterProvider, error) {
	var reader metric.Reader
	if output == "remote" {
		// recreate default registry to remove built-in collectors
		// they are covered by otel
		reg := prometheus.NewRegistry()
		prometheus.DefaultRegisterer = reg
		prometheus.DefaultGatherer = reg
		var err error
		if reader, err = promexporter.New(); err != nil {
			return nil, fmt.Errorf("failed to create metric exporter: %w", err)
		}
	} else {
		var writer io.Writer
		if output == "noop" {
			writer = NoopWriter{}
		} else {
			writer = &SlogWriter{Log: slog.Default(), Level: slog.LevelDebug}
		}
		exporter, err := stdoutmetric.New(stdoutmetric.WithWriter(writer))
		if err != nil {
			return nil, fmt.Errorf("failed to create metric exporter: %w", err)
		}
		reader = metric.NewPeriodicReader(exporter)
	}

	mp := metric.NewMeterProvider(
		metric.WithReader(reader),
		metric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	if err := host.Start(); err != nil {
		return nil, fmt.Errorf("failed to start host observer: %w", err)
	}

	if err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(5 * time.Second)); err != nil {
		return nil, fmt.Errorf("failed to start runtime observer: %w", err)
	}

	return mp, nil
}

// ConfigureLogProvider replace with OTEL log bridge when it's GA.
func ConfigureLogProvider(_ *resource.Resource, level string, format string) {
	lvl := slog.LevelDebug
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		//nolint:forbidigo,revive
		fmt.Printf("failed to parse log level: %v, fallback to DEBUG", err)
	}
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	} else {
		handler = tint.NewHandler(os.Stderr, &tint.Options{
			Level:      lvl,
			TimeFormat: time.TimeOnly,
		})
	}

	l := slog.New(slogotel.OtelHandler{Next: handler, NoTraceEvents: true})

	slog.SetDefault(l)
	otel.SetLogger(logr.FromSlogHandler(l.Handler()))
}

func TelemetryHandler(healthChecker health.Checker) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", HandleHTTPNotFound)
	mux.Handle("/metrics", PrometheusHandler())
	mux.HandleFunc("/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status":"up"}`)); err != nil {
			slog.ErrorContext(r.Context(), "failed to write response", "err", err)
		}
	})
	mux.Handle("/ready", health.NewHandler(healthChecker))
	h := RecoverMiddleware(mux)
	return h
}

func PrometheusHandler() http.Handler {
	writer := &SlogWriter{Log: slog.Default(), Level: slog.LevelError}
	return promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
		ErrorLog:          log.New(writer, "", 0),
		ErrorHandling:     promhttp.HTTPErrorOnError,
		Timeout:           5 * time.Second,
		EnableOpenMetrics: true,
		ProcessStartTime:  time.Now(),
	})
}

func HealthChecker(checks ...health.Check) health.Checker {
	checkOptions := []health.CheckerOption{
		health.WithTimeout(3 * time.Second),
		health.WithStatusListener(healthStatusListener),
		health.WithDisabledAutostart(),
	}
	for _, check := range checks {
		checkOptions = append(checkOptions, health.WithPeriodicCheck(3*time.Second, 1*time.Second, check))
	}
	return health.NewChecker(checkOptions...)
}

func healthStatusListener(ctx context.Context, state health.CheckerState) {
	attrs := []any{slog.String("status", string(state.Status))}
	for name, checkState := range state.CheckState {
		cha := []any{
			slog.String("status", string(checkState.Status)),
			slog.Time("lastCheckedAt", checkState.LastCheckedAt),
			slog.Time("lastSuccessAt", checkState.LastSuccessAt),
			slog.Time("firstCheckStartedAt", checkState.FirstCheckStartedAt),
		}
		if checkState.Result != nil {
			cha = append(cha, slog.Any("err", checkState.Result))
		}
		if !checkState.LastFailureAt.IsZero() {
			cha = append(cha, slog.Time("lastFailureAt", checkState.LastFailureAt))
		}
		if checkState.ContiguousFails > 0 {
			cha = append(cha, slog.Uint64("contiguousFails", uint64(checkState.ContiguousFails)))
		}
		g := slog.Group(name, cha...)
		attrs = append(attrs, g)
	}
	lvl := slog.LevelError
	if state.Status == health.StatusUp {
		lvl = slog.LevelInfo
	}
	slog.Log(ctx, lvl, "health status changed", attrs...)
}

func logHTTPRequest(r *http.Request, m httpsnoop.Metrics) {
	if strings.HasPrefix(r.RequestURI, "/v1/") ||
		strings.HasPrefix(r.RequestURI, "/ui/") ||
		strings.HasPrefix(r.RequestURI, "/.well-known/") ||
		strings.EqualFold(r.RequestURI, "/") {
		// skipping vault requests
		return
	}
	//nolint:errcheck
	bytesIn, _ := strconv.Atoi(r.Header.Get("Content-Length"))
	attrs := []any{
		slog.String("host", r.Host),
		slog.String("uri", r.RequestURI),
		slog.String("method", r.Method),
		slog.String("referer", r.Referer()),
		slog.Int("status", m.Code),
		slog.Int("bytesIn", bytesIn),
		slog.Int64("bytesOut", m.Written),
		slog.Duration("latency", m.Duration),
	}

	span := oteltrace.SpanFromContext(r.Context())
	if readSpan, ok := span.(trace.ReadOnlySpan); ok {
		for _, event := range readSpan.Events() {
			if event.Name == semconv.ExceptionEventName {
				var errAttrs []any
				for _, a := range event.Attributes {
					errAttrs = append(errAttrs, slog.String(string(a.Key), a.Value.AsString()))
				}
				attrs = append(attrs, slog.Group("err", errAttrs...))
				break
			}
		}
	}

	slog.InfoContext(r.Context(), "access", attrs...)
}

func labelServerRequest(ctx context.Context, requestID string) {
	var spanAttrs []attribute.KeyValue

	// copy from
	// https://github.com/open-telemetry/opentelemetry-go-contrib/blob/instrumentation/net/http/otelhttp/v0.49.0/instrumentation/net/http/otelhttp/handler.go#L273-L279
	operationAttr := semconv.HTTPRouteKey.String(requestID)
	spanAttrs = append(spanAttrs, operationAttr)

	span := oteltrace.SpanFromContext(ctx)
	span.SetAttributes(spanAttrs...)

	labeler, _ := otelhttp.LabelerFromContext(ctx)
	labeler.Add(operationAttr)
}

var telemetryURLPathPatterns = map[*regexp.Regexp]string{}

var telemetryURLPathExactMatches = []string{
	"/xmlrpc.php",
}

// Dynamic telemetry context key for per-request attributes
type dynamicTelemetryContextKeyType string

const dynamicTelemetryContextKey dynamicTelemetryContextKeyType = "dynamic.telemetry"

// DynamicTelemetryAttributes holds additional telemetry attributes to be added per request
type DynamicTelemetryAttributes struct {
	AttributeMap map[string]string
}

// WithDynamicTelemetry adds dynamic telemetry attributes to the request context
func WithDynamicTelemetry(ctx context.Context, attrs *DynamicTelemetryAttributes) context.Context {
	return context.WithValue(ctx, dynamicTelemetryContextKey, attrs)
}

// AddHTTPClientTelemetry is a convenience function for adding HTTP client-specific telemetry
func AddHTTPClientTelemetry(ctx context.Context, attributeMap map[string]string) context.Context {
	attrs := &DynamicTelemetryAttributes{
		AttributeMap: attributeMap,
	}
	return WithDynamicTelemetry(ctx, attrs)
}

func labelClientRequest(serverName string, r *http.Request) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	attrs = append(attrs, semconv.ServiceName(serverName))

	// Null safety checks to prevent crashes
	if r == nil || r.URL == nil {
		return attrs
	}

	// Add dynamic telemetry attributes from context
	if dynamicAttrs, ok := r.Context().Value(dynamicTelemetryContextKey).(*DynamicTelemetryAttributes); ok && dynamicAttrs != nil {
		// Add extra attributes
		for key, value := range dynamicAttrs.AttributeMap {
			attrs = append(attrs, attribute.String(key, value))
		}
	}

	// we cannot blindly use r.URL.Path as a label because it will cause cardinality explosion with paths like:
	// /user/f23e142c-db90-4041-b621-a23bb85b7bc4
	// so here we are going to whitelist paths with no cardinality as well as regex replace identifiers in the high-cardinality paths
	if slices.Contains(telemetryURLPathExactMatches, r.URL.Path) {
		attrs = append(attrs, semconv.HTTPRoute(r.URL.Path))
	} else {
		found := false
		for pattern, normalized := range telemetryURLPathPatterns {
			if pattern.MatchString(r.URL.Path) {
				attrs = append(attrs, semconv.HTTPRoute(normalized))
				found = true
				break
			}
		}
		if !found {
			slog.WarnContext(r.Context(), "failed to find a matching path pattern to normalize", "path", r.URL.Path)
		}
	}

	return attrs
}

// SlogWriter based on https://github.com/uber-go/zap/blob/v1.27.0/zapio/writer.go
type SlogWriter struct {
	Log   *slog.Logger
	Level slog.Level
	buff  bytes.Buffer
}

func (w *SlogWriter) Write(bs []byte) (n int, err error) {
	if !w.Log.Enabled(context.Background(), w.Level) {
		return len(bs), nil
	}
	n = len(bs)
	for len(bs) > 0 {
		bs = w.writeLine(bs)
	}
	return n, nil
}

func (w *SlogWriter) writeLine(line []byte) (remaining []byte) {
	idx := bytes.IndexByte(line, '\n')
	if idx < 0 {
		//nolint:revive
		w.buff.Write(line)
		return nil
	}
	line, remaining = line[:idx], line[idx+1:]
	if w.buff.Len() == 0 {
		w.log(line)
		return remaining
	}
	//nolint:revive
	w.buff.Write(line)
	w.flush(true)
	return remaining
}

func (w *SlogWriter) Close() error {
	return w.Sync()
}

func (w *SlogWriter) Sync() error {
	w.flush(false)
	return nil
}

func (w *SlogWriter) flush(allowEmpty bool) {
	if allowEmpty || w.buff.Len() > 0 {
		w.log(w.buff.Bytes())
	}
	w.buff.Reset()
}

func (w *SlogWriter) log(b []byte) {
	w.Log.Log(context.Background(), w.Level, string(b))
}

type NoopWriter struct{}

func (w NoopWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}
