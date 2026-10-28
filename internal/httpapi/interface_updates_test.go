package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticHTMLAlwaysServesCurrentRelease(t *testing.T) {
	dir := t.TempDir()
	index := `<html><head></head><body><script type="module" src="/assets/app-old.js"></script></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0600); err != nil {
		t.Fatal(err)
	}
	api := &Server{webDir: dir, control: controlSettings{version: "v1.0.0"}}
	handler := api.staticHandler()
	var oldETag string
	for _, path := range []string{"/", "/index.html", "/projects/example"} {
		r := httptest.NewRequest(http.MethodGet, "http://yard.test"+path, nil)
		r.Header.Set("If-Modified-Since", "Wed, 31 Dec 2099 23:59:59 GMT")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(w.Body.String(), `name="yard-version" content="v1.0.0"`) {
			t.Fatalf("stale/missing release HTML at %s: %d %v %s", path, w.Code, w.Header(), w.Body.String())
		}
		oldETag = w.Header().Get("ETag")
	}
	api.control.version = "v1.0.1"
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(strings.ReplaceAll(index, "app-old.js", "app-new.js")), 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://yard.test/", nil)
	r.Header.Set("If-Modified-Since", "Wed, 31 Dec 2099 23:59:59 GMT")
	r.Header.Set("If-None-Match", oldETag)
	handler.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("ETag") == oldETag || !strings.Contains(w.Body.String(), `content="v1.0.1"`) || !strings.Contains(w.Body.String(), "app-new.js") || strings.Contains(w.Body.String(), "app-old.js") {
		t.Fatalf("deployment returned a stale entry point: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "http://yard.test/", nil))
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("HEAD must have the same cache policy without a body")
	}
}

func TestStaticFilesRevalidateAndVersionIsEscaped(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"index.html": "<html><head></head><body></body></html>", "app.js": "export const release = 1;"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	api := &Server{webDir: dir, control: controlSettings{version: `release"><script>unsafe</script>`}}
	handler := api.staticHandler()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://yard.test/app.js", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" || w.Body.String() != "export const release = 1;" {
		t.Fatal("static assets must revalidate without changing their contents")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://yard.test/", nil))
	if strings.Contains(w.Body.String(), "<script>unsafe</script>") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatal("release metadata must be escaped inside the HTML attribute")
	}
}
