package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticAssetsNeverServeEnvironmentFiles(t *testing.T) {
	web := t.TempDir()
	const secret = "TOKEN=private-static-fixture"
	for name, content := range map[string]string{
		"index.html":                "<html>Yard</html>",
		"assets/app.js":             "console.log('Yard')",
		".env":                      secret,
		".env.production":           secret,
		"config/app.env":            secret,
		"config/app.env.bak":        secret,
		"config/APP.ENV":            secret,
		".git/config":               secret,
		"assets/.private/token.txt": secret,
	} {
		file := filepath.Join(web, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	handler := (&Server{webDir: web}).Handler()
	for _, path := range []string{
		"/.env", "/.env.production", "/%2eenv", "/config/app.env",
		"/config/app.env.bak", "/config/APP.ENV", "/config/app%2eenv",
		"/.git/config", "/assets/.private/token.txt", "/%2e%2e/.env",
		"/assets%5c..%5c.env",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+path, func(t *testing.T) {
				result := httptest.NewRecorder()
				handler.ServeHTTP(result, httptest.NewRequest(method, "http://yard.test"+path, nil))
				if result.Code != http.StatusNotFound || strings.Contains(result.Body.String(), secret) {
					t.Fatalf("private file request returned %d: %s", result.Code, result.Body.String())
				}
			})
		}
	}
	for path, content := range map[string]string{
		"/":              "<html>Yard</html>",
		"/projects/demo": "<html>Yard</html>",
		"/assets/app.js": "console.log('Yard')",
	} {
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest(http.MethodGet, "http://yard.test"+path, nil))
		if result.Code != http.StatusOK || result.Body.String() != content {
			t.Fatalf("public asset %s returned %d: %s", path, result.Code, result.Body.String())
		}
	}
}

func TestStaticAssetsCannotFollowSymlinksOutsideWebDirectory(t *testing.T) {
	web, private := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("Yard"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, ".env"), []byte("TOKEN=private-symlink-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(private, ".env"), filepath.Join(web, "download.txt")); err != nil {
		t.Skipf("Symlinks are unavailable: %v", err)
	}
	result := httptest.NewRecorder()
	(&Server{webDir: web}).Handler().ServeHTTP(result, httptest.NewRequest(http.MethodGet, "http://yard.test/download.txt", nil))
	if result.Code != http.StatusNotFound || strings.Contains(result.Body.String(), "private-symlink-fixture") {
		t.Fatalf("external symlink returned %d: %s", result.Code, result.Body.String())
	}
}
