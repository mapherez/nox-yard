// Package noxlogin handles Yard's NoX Login enrollment. The administrative
// socket is mounted only in the one-shot setup container, never the runtime.
package noxlogin

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"strings"
	"time"

	noxauth "github.com/mapherez/nox-auth/client"
	"github.com/mapherez/nox-yard/internal/store"
)

const CallbackPath = "/auth/nox/callback"

func Setup(ctx context.Context, data *store.Store, issuer, origin, adminSocket, runtimeSocket string) error {
	issuer = strings.TrimRight(issuer, "/")
	origin = strings.TrimRight(origin, "/")
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(origin, "#") {
		return errors.New("NOX_PUBLIC_URL must be the configured browser-facing HTTP(S) origin for NoX Login")
	}
	old, found, err := data.NoXLoginConfiguration()
	if err != nil {
		return err
	}
	secret, clientID := old.Secret, old.ClientID
	if !found {
		secret = noxauth.GenerateSecret()
		clientID = "nox-yard-" + strings.ToLower(rand.Text())
	}
	config := store.NoXLoginConfig{Issuer: issuer, Origin: origin, ClientID: clientID, Secret: secret, SocketPath: runtimeSocket}
	if _, err = noxauth.New(noxauth.Config{Issuer: issuer, ClientID: config.ClientID, ClientSecret: secret, SocketPath: runtimeSocket}); err != nil {
		return err
	}
	if found && old.Issuer != issuer {
		return errors.New("Auth issuer differs from the enrolled identity; restore NOX_AUTH_PUBLIC_URL rather than silently replacing the issuer")
	}
	// Persist before delivery so a lost response/restart reuses the same secret.
	if err = data.SaveNoXLoginConfiguration(config); err != nil {
		return err
	}
	for {
		attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = noxauth.Register(attempt, adminSocket, config.ClientID, []string{origin + CallbackPath}, secret, false)
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("NoX Auth enrollment unavailable: confirm Auth is running and both projects share the configured local volumes")
		case <-time.After(time.Second):
		}
	}
}
