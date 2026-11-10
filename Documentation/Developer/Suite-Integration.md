# Same-host Suite installation and lifecycle

Yard keeps its existing Docker/Compose deployment, configuration, workers and
recovery. The Suite adapter adds a private Unix listener and post-deployment
registration; it does not introduce another deployment manager or product catalog.

## Foundation setup and authority

Use the shipped Compose with the same external local-channel volume as Backend
and Auth. Its runtime and one-shot setup explicitly configure
`NOX_YARD_SUITE_SOCKET_DIR=/run/nox-suite`. `NOX_SUITE_LOCAL_VOLUME_NAME` optionally
selects the installation's existing volume; no host-specific IP, certificate,
manual credential file or extra user approval is required. Ordinary standalone
processes without that channel keep native operation and direct MCP independent.

In Backend administration, save Yard's native HTTP/HTTPS origin, Verify and
Connect. Backend contacts Yard through `nox-yard.sock`, not through that web URL.
Yard persists pairing proof/provisioning before acknowledgement and recovers the
same association after restart. The declaration is public metadata; serving it
alone does not prove membership. `/mcp/suite` is served only on the local listener;
native web requests and forged forwarded/transport headers do not grant access.

The existing NoX Login identifies the user. Administrative pairing delegates the
installer's associated pairing, registry and lifecycle scopes. Yard subsequently
uses its service credential to manage only its own local deployment associations.
There is no forwarded personal Auth token, per-install account approval, or grant
of Backend account/key administration. Backend rejects foreign installer/service
paths, target substitution, competing claims, standalone/foundation takeovers and
noncanonical origins. All normal native-session, CSRF, MCP and operation checks
remain in their existing entry points.

## Participating Compose services

A conforming product's service declares this marker:

```yaml
services:
  application:
    image: example/product:version
    labels:
      io.nox.suite.product-id: actual-product-id
```

Through the existing preview/deployment path, Yard prepares the shared external
volume at `/run/nox-suite` for that service only. An explicit existing mount must
resolve to the same writable named volume. Ordinary unmarked services/companions
are left untouched. IDs must be SDK-valid and unique per project; the participant
service must have exactly one replica. The product image must implement its own
local listener, declaration and idempotent pairing receiver. A marker cannot make
an unadapted image integrated, grant trust, or supply MCP tools that the product does not implement.
The convention works for any conforming product, not a fixed product list.

## Durable registration and lifecycle

Existing workers commit successful deployment results and Suite candidates/outbox
in the same SQLite transaction. A separate reconciler consumes that outbox, then
inspects the exact project/service/product labels. It obtains its own scoped
association, delivers the product pairing, confirms acknowledgement, registers
the declaration and reports observations with the current producer epoch.

- A Docker success and a pending Suite integration are different durable results.
  Backend/product outages do not replay a successful Docker installation.
- Updates refresh a materially changed declaration/association. Stop/start retains
  membership; stopped products can be reported without an active product socket.
- A transiently missing container during an active recreate/update job does not
  retire membership. Recovery-required jobs keep their existing reservation.
- Removing the project/participating service retires its registry association and
  grant. Removing an ordinary companion does not retire the product. Source marker
  changes retire the old binding before replacing it.
- Removal preserves host source files and application data under the existing
  removal choices. A reinstall uses a new deployment ID and current trust revision;
  it does not seize another installer's or an independent product's identity.
- Mutation keys and private recovery state survive restart. A confirmed existing
  registration recovers a lost response without repeating the mutation with new
  preconditions. Delayed removal does not delete a replacement association.

Settings → NoX Suite shows connection and per-deployment integration state.
`Retry integration` resumes only this integration journal; it does not rerun the
Docker job. The existing session/CSRF protection and displayed revision apply.
Private credentials/intents/recovery fields are never returned in this view.

Native routes: `GET /nox/integration`, authenticated `GET /api/suite`, and
session/CSRF-protected `POST /api/suite/deployments/{id}/retry` with
`{"revision": <current revision>}`. A stale revision returns 409.

## Verification and release order

`python3 scripts/smoke-suite.py --image <yard-image>` runs real foundation pairing,
ACK, registration, native boundary and restart against published Backend v1.2.0
and Auth v0.0.2, using isolated state. It is included in the shared CI/release
runtime inventory. Published Backend v1.2.0 supports that foundation flow but
predates scoped installer deployment management.

For complete lifecycle acceptance, build Backend from the corresponding updated
source and use:

```sh
docker build -t backend-suite-test /path/to/nox-backend/backend
python3 scripts/smoke-suite.py --image <yard-image> --backend-image backend-suite-test --lifecycle
```

That acceptance uses the real Yard manager/worker/controller, real Backend/Auth,
and a disposable generic protocol participant. It checks volume isolation,
installation, observations, stop/start, declaration refresh, restart recovery,
removal/reinstall and Backend outage without Docker-job replay. Fixture source,
containers, volumes, networks and runner image are cleaned; it does not deploy a
user stack or contact Google. Existing browser/native regression and full source
checks remain required. Real Google/global logout was separately owner-accepted.

Use compatible Backend/yard releases for scoped installer management. The menu
adapter consumes published SDK v0.7.3; it does not require another Auth release,
new Compose settings or an additional service. The separate Gateway acceptance
uses an image with SDK 0.7.3 and the point-2 response/error corrections:

```sh
python3 scripts/smoke-suite.py --image <yard-image> --backend-image ghcr.io/mapherez/nox-backend:v1.2.2 --gateway-image <gateway-image>
```

That optional gate verifies 38 canonical Yard menu definitions through the actual
Gateway, omission of menu paths from ordinary listing, status execution and
preview/confirmation on a uniquely created disposable container. It checks that
a rejected confirmation leaves it intact before confirming that target's removal.
No installed workload or personal credential is used. Definition/permission and
pre-menu tool-contract regressions are in the required Go suite. Gateway/full
lifecycle image coverage remains distinct from the default foundation CI fixture;
passing a local candidate is not a claim about publication or the full Suite.
