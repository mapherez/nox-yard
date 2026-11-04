package suite

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/mapherez/nox-yard/internal/suite/local"
)

func TestLocalListenerDeclarationIsolationAndCleanup(t *testing.T) {
	directory, err := os.MkdirTemp("", "ys")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	controller, err := New(Config{Directory: directory, ReportInterval: time.Second}, data, "acceptance", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := controller.Start(ctx, http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	peer, err := local.NewLocal(directory, "http://nox-yard")
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	response, err := peer.HTTP.Get("http://nox-yard/nox/integration")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readBody(response)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := noxmcp.LoadSuiteProductDeclaration(raw)
	if err != nil || response.StatusCode != 200 || declaration.ProductID != "nox-yard" || declaration.ProductVersion != "acceptance" {
		t.Fatal("invalid local declaration", err)
	}
	request, _ := http.NewRequest("GET", "http://nox-yard/nox/integration", nil)
	request.Host = "another-product"
	response, err = peer.HTTP.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatal("forged destination admitted")
	}
	if duplicate, err := controller.Start(ctx, http.NotFoundHandler()); err == nil {
		duplicate()
		t.Fatal("listener replaced by a second owner")
	}
	stop()
	if _, err := os.Stat(filepath.Join(directory, "nox-yard.sock")); !os.IsNotExist(err) {
		t.Fatal("owned socket not removed", err)
	}
}
func TestSuiteDisabledDoesNotCreateLocalListener(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	controller, err := New(Config{}, data, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	stop, err := controller.Start(context.Background(), http.NotFoundHandler())
	if err != nil {
		t.Fatal(err)
	}
	stop()
	if controller.Ready() {
		t.Fatal("disabled integration claims readiness")
	}
}
