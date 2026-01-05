package pkg

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"alexejk.io/go-xmlrpc"
	"github.com/slamdev/external-dns-pfsense-webhook/api/externaldnsapi"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/business"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/business/svc"

	"github.com/slamdev/external-dns-pfsense-webhook/configs"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/integration"

	healthlib "github.com/alexliesenfeld/health"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
)

type App interface {
	Start() error
	Stop() error
}

type app struct {
	config         configs.Config
	actuatorServer integration.HTTPServer
	webhookServer  integration.HTTPServer
	traceProvider  *trace.TracerProvider
	metricProvider *metric.MeterProvider
	healthChecker  healthlib.Checker
	pfsenseClient  *xmlrpc.Client
}

func NewApp() (App, error) {
	ctx := context.Background()
	app := app{}

	if err := integration.BuildConfig("APP_", "application", configs.Configs, &app.config); err != nil {
		return nil, fmt.Errorf("failed to populate config; %w", err)
	}

	if err := app.configureTelemetry(ctx); err != nil {
		return nil, fmt.Errorf("failed to configure telemetry; %w", err)
	}

	if err := app.configurePfsenseClient(); err != nil {
		return nil, fmt.Errorf("failed to configure pfsense client; %w", err)
	}

	app.configureHealthChecker()

	pfsenseSvc := svc.NewPfsenseService(app.pfsenseClient, app.config.DryRun)

	webhookController := business.NewController(pfsenseSvc)
	webhookMux := http.NewServeMux()
	if err := app.injectWebookHandler(webhookMux, webhookController); err != nil {
		return nil, fmt.Errorf("failed to create webhook handler; %w", err)
	}

	app.webhookServer = integration.NewHTTPServer(app.config.HTTP.Port, integration.APIHandler(webhookMux))
	app.actuatorServer = integration.NewHTTPServer(app.config.Actuator.Port, integration.TelemetryHandler(app.healthChecker))
	return &app, nil
}

func (a *app) injectWebookHandler(mux *http.ServeMux, controller externaldnsapi.StrictServerInterface) error {
	swagger, err := externaldnsapi.GetSwagger()
	if err != nil {
		return fmt.Errorf("failed to get embedded swagger spec; %w", err)
	}
	apiCfg, err := integration.CreateAPIConfig(swagger)
	if err != nil {
		return fmt.Errorf("failed to create api config; %w", err)
	}

	h := externaldnsapi.NewStrictHandlerWithOptions(controller,
		apiCfg.StrictMiddlewares,
		externaldnsapi.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  apiCfg.RequestErrorHandlerFunc,
			ResponseErrorHandlerFunc: apiCfg.ResponseErrorHandlerFunc,
		},
	)
	// even though `MiddlewareFunc` is an alias of `func(http.Handler) http.Handler`
	// but `[]func(http.Handler) http.Handler` is not cast-able to `[]MiddlewareFunc` due to Go's type system
	// so we need to convert it manually; for that it is enough to simply iterate over the slice
	middlewares := integration.MapSlice(apiCfg.Middlewares, func(i func(http.Handler) http.Handler) externaldnsapi.MiddlewareFunc {
		return i
	})
	opts := externaldnsapi.StdHTTPServerOptions{
		BaseURL:          apiCfg.BaseURL,
		BaseRouter:       mux,
		Middlewares:      middlewares,
		ErrorHandlerFunc: apiCfg.ErrorHandlerFunc,
	}
	// add the handler to the mux on the base URL path
	externaldnsapi.HandlerWithOptions(h, opts)
	return nil
}

func (a *app) configureHealthChecker() {
	healthChecks := []healthlib.Check{
		integration.PfsenseHealthCheck(a.pfsenseClient),
	}
	a.healthChecker = integration.HealthChecker(healthChecks...)
}

func (a *app) configureTelemetry(ctx context.Context) error {
	telemetryResource := integration.CreateTelemetryResource(ctx)

	integration.ConfigureLogProvider(telemetryResource, a.config.Telemetry.Logs.Level, a.config.Telemetry.Logs.Format)

	var err error
	a.traceProvider, err = integration.ConfigureTraceProvider(ctx, telemetryResource, a.config.Telemetry.Traces.Output)
	if err != nil {
		return fmt.Errorf("failed to init tracer; %w", err)
	}

	a.metricProvider, err = integration.ConfigureMetricProvider(ctx, telemetryResource, a.config.Telemetry.Metrics.Output)
	if err != nil {
		return fmt.Errorf("failed to init metric provider; %w", err)
	}

	return nil
}

func (a *app) configurePfsenseClient() error {
	pfsenseURL := url.URL(a.config.Pfsense.URL)
	pfsenseClient, err := integration.CreatePfsenseClient(pfsenseURL.String(), a.config.Pfsense.Username, a.config.Pfsense.Password, a.config.Pfsense.Insecure)
	if err != nil {
		return fmt.Errorf("failed to create pfsense client; %w", err)
	}
	a.pfsenseClient = pfsenseClient
	return nil
}

func (a *app) Start() error {
	starters := []func() error{
		a.actuatorServer.Start,
		a.webhookServer.Start,
		func() error { a.healthChecker.Start(); return nil },
	}
	done := make(chan error, len(starters))
	for i := range starters {
		starter := starters[i]
		go func(starter func() error) {
			done <- starter()
		}(starter)
	}

	for range cap(done) {
		if err := <-done; err != nil {
			return err
		}
	}

	return nil
}

func (a *app) Stop() error {
	a.healthChecker.Stop()
	ctx := context.Background()

	err := errors.Join(
		a.actuatorServer.Stop(ctx),
		a.webhookServer.Stop(ctx),
		a.traceProvider.Shutdown(ctx),
		a.metricProvider.Shutdown(ctx),
	)
	return err
}
