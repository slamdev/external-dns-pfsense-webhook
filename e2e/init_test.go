//nolint:revive
package e2e

import (
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/slamdev/external-dns-pfsense-webhook/pkg"

	"github.com/slamdev/external-dns-pfsense-webhook/pkg/integration"
	"github.com/slamdev/external-dns-pfsense-webhook/testdata"
)

//go:embed *.yaml
var ConfigsFS embed.FS

//nolint:revive
func TestMain(m *testing.M) {
	if err := integration.BuildConfig("E2E_", "e2e", ConfigsFS, &testdata.Cfg); err != nil {
		slog.Error("failed to build config", "error", err)
		os.Exit(1)
	}

	var stopApp func() error
	if testdata.Cfg.App.AutoStart {
		stopApp = startApp()
	}

	code := m.Run()
	if stopApp != nil {
		if err := stopApp(); err != nil {
			slog.Error("failed to stop app", "err", err)
			os.Exit(1)
		}
	}
	os.Exit(code)
}

func startApp() func() error {
	//pfsenseURL, pfsenseCloseFunc := testdata.MockPfsenseServer()

	//os.Setenv("APP_PFSENSE_URL", pfsenseURL)

	appPort := testdata.GetFreePort()
	os.Setenv("APP_HTTP_PORT", strconv.Itoa(appPort))
	testdata.Cfg.App.URL = fmt.Sprintf("http://localhost:%d", appPort)

	actuatorPort := testdata.GetFreePort()
	os.Setenv("APP_ACTUATOR_PORT", strconv.Itoa(actuatorPort))

	app, err := pkg.NewApp()
	if err != nil {
		slog.Error("failed to start app", "err", err)
		os.Exit(1)
	}
	go func() {
		if err := app.Start(); err != nil {
			slog.Error("failed to start app", "err", err)
			os.Exit(1)
		}
	}()
	for {
		r, err := http.Get(fmt.Sprintf("http://localhost:%d/ready", actuatorPort)) //nolint:noctx
		if err == nil && r.StatusCode == http.StatusOK {
			break
		}
		r.Body.Close()
		time.Sleep(1 * time.Second)
	}

	slog.Info("app started for e2e tests", "appUrl", testdata.Cfg.App.URL)

	return func() error {
		//pfsenseCloseFunc()
		return app.Stop()
	}
}
