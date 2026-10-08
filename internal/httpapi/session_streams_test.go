package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mapherez/nox-yard/internal/inventory"
)

type sessionStreamStub struct {
	inventory.TerminalManager
	reader *io.PipeReader
	writer *io.PipeWriter
	closed chan struct{}
	once   sync.Once
	calls  int
}

func newSessionStreamStub() *sessionStreamStub {
	r, w := io.Pipe()
	return &sessionStreamStub{reader: r, writer: w, closed: make(chan struct{})}
}
func (s *sessionStreamStub) close() {
	s.once.Do(func() { s.reader.Close(); s.writer.Close(); close(s.closed) })
}
func (s *sessionStreamStub) OpenLogs(context.Context, string) (inventory.LogStream, error) {
	return inventory.LogStream{Reader: s.reader, TTY: true}, nil
}
func (s *sessionStreamStub) OpenTerminal(context.Context, string, uint, uint) (inventory.TerminalSession, error) {
	s.calls++
	return inventory.TerminalSession{ID: "fixture", Reader: s.reader, Writer: io.Discard, Close: s.close}, nil
}

func TestStreamsCloseWhenSessionRevoked(t *testing.T) {
	for _, kind := range []string{"events", "logs", "terminal"} {
		for _, revoke := range []string{"logout", "reset", "expiry"} {
			t.Run(kind+"/"+revoke, func(t *testing.T) {
				data, api := newTestServer(t, t.TempDir(), "")
				defer data.Close()
				api.sessionCheckInterval = 20 * time.Millisecond
				stub := newSessionStreamStub()
				defer stub.close()
				api.SetInventory(&liveInventoryStub{})
				api.SetLogs(stub)
				api.SetTerminal(stub)
				handler := api.Handler()
				setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
				if setup.Code != 201 {
					t.Fatal("setup failed")
				}
				cookie := setup.Result().Cookies()[0]
				var session bootstrapResponse
				if err := json.Unmarshal(setup.Body.Bytes(), &session); err != nil {
					t.Fatal(err)
				}
				host := httptest.NewServer(handler)
				defer host.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				revokeSession := func() {
					t.Helper()
					switch revoke {
					case "logout":
						req := httptest.NewRequest("POST", "http://yard.test/api/logout", nil)
						req.AddCookie(cookie)
						req.Header.Set("Origin", "http://yard.test")
						req.Header.Set("X-CSRF-Token", session.CSRFToken)
						response := httptest.NewRecorder()
						handler.ServeHTTP(response, req)
						if response.Code != 204 {
							t.Fatal("logout failed")
						}
					case "reset":
						if err := data.ResetAdminPassword("replacement-hash"); err != nil {
							t.Fatal(err)
						}
					case "expiry":
						if err := data.DeleteSession(hashToken(cookie.Value)); err != nil {
							t.Fatal(err)
						}
						if err := data.CreateSession(hashToken(cookie.Value), session.CSRFToken, time.Now().Add(-time.Second)); err != nil {
							t.Fatal(err)
						}
					}
				}
				path := "/api/projects/events"
				if kind != "events" {
					path = "/api/containers/" + strings.Repeat("a", 64) + "/" + kind
				}
				if kind == "terminal" {
					headers := http.Header{}
					headers.Set("Cookie", cookie.String())
					headers.Set("Origin", host.URL)
					conn, _, err := websocket.Dial(ctx, host.URL+path, &websocket.DialOptions{HTTPHeader: headers})
					if err != nil {
						t.Fatal(err)
					}
					defer conn.CloseNow()
					payload, _ := json.Marshal(terminalMessage{Type: "open", CSRFToken: session.CSRFToken, Cols: 80, Rows: 24})
					if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
						t.Fatal(err)
					}
					_, ready, err := conn.Read(ctx)
					if err != nil || !strings.Contains(string(ready), `"ready"`) {
						t.Fatal("terminal not ready")
					}
					revokeSession()
					_, _, err = conn.Read(ctx)
					if websocket.CloseStatus(err) != terminalSessionExpired {
						t.Fatalf("terminal close = %v", err)
					}
					select {
					case <-stub.closed:
					case <-ctx.Done():
						t.Fatal("terminal Docker stream not released")
					}
				} else {
					req, _ := http.NewRequestWithContext(ctx, "GET", host.URL+path, nil)
					req.AddCookie(cookie)
					response, err := host.Client().Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer response.Body.Close()
					reader := bufio.NewReader(response.Body)
					for {
						line, err := reader.ReadString('\n')
						if err != nil {
							t.Fatal(err)
						}
						if line == "\n" {
							break
						}
					}
					revokeSession()
					remaining, err := io.ReadAll(reader)
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(string(remaining), "event: session-expired") {
						t.Fatal("stream did not report session expiry")
					}
					if kind == "logs" {
						if _, err := stub.writer.Write([]byte("after revocation\n")); err == nil {
							t.Fatal("Docker log stream still open")
						}
					}
				}
			})
		}
	}
}

func TestTerminalRechecksSessionAfterHandshake(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	stub := newSessionStreamStub()
	defer stub.close()
	api.SetTerminal(stub)
	handler := api.Handler()
	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	cookie := setup.Result().Cookies()[0]
	var session bootstrapResponse
	json.Unmarshal(setup.Body.Bytes(), &session)
	host := httptest.NewServer(handler)
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	headers := http.Header{}
	headers.Set("Cookie", cookie.String())
	headers.Set("Origin", host.URL)
	conn, _, err := websocket.Dial(ctx, host.URL+"/api/containers/"+strings.Repeat("a", 64)+"/terminal", &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := data.DeleteSession(hashToken(cookie.Value)); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(terminalMessage{Type: "open", CSRFToken: session.CSRFToken, Cols: 80, Rows: 24})
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatal(err)
	}
	_, _, err = conn.Read(ctx)
	if websocket.CloseStatus(err) != terminalSessionExpired || stub.calls != 0 {
		t.Fatal("revoked handshake opened Docker shell")
	}
}

func TestLoginRateLimitUsesPeerIPAndRecovers(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	handler := api.Handler()
	if setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword); setup.Code != 201 {
		t.Fatal("setup failed")
	}
	login := func(ip, password string) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(credentials{Username: "owner", Password: password})
		req := httptest.NewRequest("POST", "http://yard.test/api/login", strings.NewReader(string(payload)))
		req.RemoteAddr = ip + ":1234"
		req.Header.Set("Origin", "http://yard.test")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "192.0.2.99")
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		return result
	}
	for i := 0; i < 5; i++ {
		if result := login("192.0.2.1", "wrong password"); result.Code != 401 {
			t.Fatal("bad credentials not counted")
		}
	}
	if result := login("192.0.2.1", testPassword); result.Code != 429 || result.Header().Get("Retry-After") != "900" {
		t.Fatal("rate limit missing")
	}
	if result := login("192.0.2.2", testPassword); result.Code != 200 {
		t.Fatal("forwarded header changed limiter identity")
	}
	api.limiter.mu.Lock()
	attempt := api.limiter.entries["192.0.2.1"]
	attempt.until = time.Now().Add(-time.Second)
	api.limiter.entries["192.0.2.1"] = attempt
	api.limiter.mu.Unlock()
	if result := login("192.0.2.1", testPassword); result.Code != 200 {
		t.Fatal("expired rate limit did not recover")
	}
	for i := 0; i < 4; i++ {
		login("192.0.2.1", "wrong")
	}
	if result := login("192.0.2.1", testPassword); result.Code != 200 {
		t.Fatal("success rejected")
	}
	for i := 0; i < 5; i++ {
		if result := login("192.0.2.1", "wrong"); result.Code != 401 {
			t.Fatal("successful login did not clear failures")
		}
	}
}
