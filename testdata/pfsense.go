package testdata

import (
	"net"
	"net/http"
	"net/http/httptest"

	"github.com/slamdev/external-dns-pfsense-webhook/pkg/integration"
)

func MockPfsenseServer() (string, func()) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", integration.HandleHTTPNotFound)

	srv := httptest.NewUnstartedServer(mux)
	// httptest server binds to 127.0.0.1 so it is not accessible from docker containers
	// we need to bind to 0.0.0.0
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		panic(err)
	}
	srv.Listener = l
	srv.Start()
	return srv.URL, srv.Close
}
