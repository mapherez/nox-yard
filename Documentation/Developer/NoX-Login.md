# NoX Login in Yard

Yard uses the published `github.com/mapherez/nox-auth/client` **v0.0.1**. Google credentials and allowed accounts belong only to NoX Auth. Yard displays **NoX Login**, validates the OIDC code result and creates its own server-side session. `/api/setup` and `/api/login` no longer provide dashboard entry. There is no local-password fallback.

## Deployment

Run Auth first on the same Docker host. Update Yard's Compose and set:

```dotenv
NOX_PUBLIC_URL=http://yard.example.test
NOX_AUTH_PUBLIC_URL=https://auth.example.test
```

Use your real browser-facing origins. Yard may use a private HTTP hostname/IP or HTTPS. Google Console needs only Auth's callback; do not create a separate Google client for Yard. No manual client secret or account list is required in Yard's environment.

The project's `auth-setup` service runs **once before Yard starts**, using the same Yard image with `nox-yard auth-setup`. It generates and persists a unique OAuth client ID for this Yard installation and a client secret, registers the exact `${NOX_PUBLIC_URL}/auth/nox/callback` through Auth's administrative Unix socket, then exits successfully. Repeated setup reuses that identity/secret and is idempotent. Separate Yard data directories receive separate OAuth identities, so a development/validation instance cannot replace the production callback. These OAuth client IDs do not change Yard's Suite product ID. `Exited (0)` for this service is expected.

Only this setup component mounts `nox-auth-admin`. The ordinary Yard service mounts `nox-suite-local`, its own data and the existing Docker socket; it does not mount Auth's administrative volume. Mounting the admin volume is an explicit deployment capability. This registers Yard's login client, not a Backend deployment grant, and does not let a Yard service token grant itself Backend administration.

The default volume names match the Auth Compose. If Auth uses custom names, set the same `NOX_SUITE_LOCAL_VOLUME_NAME` and `NOX_AUTH_ADMIN_VOLUME_NAME` in Yard. The admin volume is external and must already exist. Auth, Yard and the setup component must use the same host; public token exchange or remote app enrollment is not introduced.

Changing Yard's canonical URL means updating its environment and recreating the setup/service through Compose, so the approved callback is updated together with the browser Origin/cookie policy. An issuer change is not silently accepted as another identity provider. Native development uses the same flow through `compose.dev.yaml`, with Vite's configured origin as the callback origin.

## Browser and session behavior

`GET /auth/nox/start` creates a short-lived transaction containing state, nonce, PKCE verifier and a safe local return path. A separate HttpOnly, SameSite=Lax cookie binds the transaction to that browser. An exact configured callback is used; Host/forwarded headers cannot choose an arbitrary callback. External return URLs, replay and unbound callbacks are rejected.

`GET /auth/nox/callback` atomically consumes the transaction and exchanges the code over the shared local socket. The released client validates issuer, signature, audience, expiry, nonce and access-token binding. Auth credentials stay in private SQLite session storage. The browser receives only Yard's opaque session cookie and its CSRF token through the authenticated bootstrap response. HTTPS origins use a Secure `__Host-` cookie; HTTP origins remain usable.

Sessions last one year, renewable with use. Yard checks current account/session admission through local Auth and reuses it for no more than five minutes. Renewal starts slightly early to leave time for the existing 15-second stream heartbeat and bounded Auth request. It does not repeat Google login. Expired validation cannot be extended on outage or denial. Active SSE/terminal connections retain their existing expiry/revocation checks.

The browser periodically checks whether the Auth cookie needs a normal SSO redirect before its expiry; an already valid Auth session returns without another Google account selection. Application cookies and persisted Auth credentials remain independent of another device's login.

`POST /api/logout` retains Origin and CSRF validation. It revokes the app credential when reachable and always removes the local Yard session/cookie on successful local sign-out. It leaves Auth SSO and other apps intact. Suite-wide logout belongs to Backend/Portal, not a new Yard or Auth management screen. Existing direct MCP and Control API tokens/policies are unchanged.

`/healthz` checks storage, not Google login or successful enrollment. Missing enrollment is shown as an unavailable NoX Login in bootstrap/UI; it never enables the old password entry points. The production Compose prevents the runtime service starting before successful enrollment.

## Data and rollback

The administrator record/password hash, projects, configuration, jobs, machine settings and existing client-token state are retained. NoX client configuration, transactions and sessions use additional tables without increasing `PRAGMA user_version`; previous supported Yard images can ignore those tables. Legacy password sessions are not accepted by this image.

Retain `./data` and existing project/application storage. To roll back, restore the previous image **and its previous Compose**: that image does not implement the new `auth-setup` command. The old image can then use the original local account. An old installation's existing administrator hash is not rewritten by NoX Login. The retained `reset-admin-password` command updates only that legacy recovery record; it does not change Google credentials or revoke current Auth sessions.

No separate data-import project or database reset is required. The isolated `smoke-nox-login.py` gate tests this with the published previous image v1.3.13 and Auth v0.0.1: it initializes a local account/settings, enrolls and starts current Yard, then reopens the same data in the old image and authenticates the unchanged local account. A very old image with an incompatible schema is not made compatible by these additive tables.

## Verification boundaries

HTTP tests exercise the real released OIDC client against a controlled signed-token provider, callback/browser binding, code replay, local logout, account revocation, session persistence, old-cookie rejection and unchanged legacy administrator data. Existing domain/MCP tests use explicit verified Auth fixtures rather than the removed password routes.

Runtime acceptance uses a separate test-only Go provider, compiled in a helper container that sees only a labelled disposable Yard data mount. Its seed API is protected and loopback-published; it is not included in the Yard image and never provisions an installed Yard. Normal production builds cannot enable those fixture endpoints. Existing Docker/MCP test suites retain their operation and recovery checks.

A real Google-account login on the owner's installation remains a separate acceptance step after publishing/deploying the image. Source fixtures or `/healthz` do not certify that login. Yard's unfinished Suite pairing/registration/lifecycle work remains separate; this login adaptation does not complete correction points 7 and 8.

The Docker regressions also exercise the shared bind-source proof used during restoration. Docker Desktop may report a daemon path alias after recreation. Comparison accepts it only after a read-only device/inode/type proof; failed or empty `stat` results refuse that proof. Mount type, destination, write access and options still compare separately. See [local verification](Progress.md#local-verification-of-nox-login) for exact image/evidence boundaries.
