package httpapi

import (
	"net/http/httptest"
	"testing"

	"github.com/mapherez/nox-yard/internal/store"
	"github.com/mapherez/nox-yard/internal/suite"
)

func TestSuiteMetadataAndTCPBoundary(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	server, err := New(data, t.TempDir(), "http://yard.example.test")
	if err != nil {
		t.Fatal(err)
	}
	controller, err := suite.New(suite.Config{}, data, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	server.SetSuite(controller)
	handler := server.Handler()
	for _, test := range []struct {
		method, path string
		status       int
	}{{"GET", "/nox/integration", 200}, {"GET", "/api/suite", 401}, {"POST", "/api/suite/deployments/anything/retry", 401}, {"GET", "/mcp/suite", 404}, {"POST", "/mcp/suite", 404}} {
		request := httptest.NewRequest(test.method, "http://yard.example.test"+test.path, nil)
		request.Header.Set("Origin", "http://yard.example.test")
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Header.Set("X-Nox-Suite-Transport", "unix")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("%s %s: %d %s", test.method, test.path, response.Code, response.Body.String())
		}
	}
}
