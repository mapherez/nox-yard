"""NoX identity dependency for explicitly labelled, disposable Docker fixtures.

No endpoint or test authentication switch is added to the production Yard image.
The helper is compiled from source in a separate Go container and sees only the
fixture's data mount. Its seed endpoint is protected and published on loopback.
"""
import atexit
import http.cookiejar
import json
import pathlib
import secrets
import subprocess
import time
import urllib.request
import urllib.parse

_helpers = {}


def _docker(*args):
    return subprocess.check_output(['docker', *args], text=True, stderr=subprocess.PIPE).strip()


def cleanup_source(source):
    for key, helper in list(_helpers.items()):
        if key == source or key.startswith(str(source).rstrip('/') + '/'):
            subprocess.run(['docker', 'rm', '-f', helper['id']], capture_output=True, check=True)
            _helpers.pop(key, None)


def cleanup_all():
    for key in list(_helpers):
        cleanup_source(key)


atexit.register(cleanup_all)


def login(origin, docker=None):
    parsed = urllib.parse.urlparse(origin)
    if parsed.scheme != 'http' or parsed.hostname != '127.0.0.1' or not parsed.port:
        raise RuntimeError('Auth fixture requires an explicit loopback HTTP test origin')
    ids = _docker('ps', '-q').split()
    target = None
    for item in json.loads(_docker('inspect', *ids)) if ids else []:
        ports = item.get('NetworkSettings', {}).get('Ports', {}).get('8080/tcp') or []
        if any(entry['HostPort'] == str(parsed.port) and entry['HostIp'] in ('127.0.0.1', '0.0.0.0') for entry in ports):
            target = item
            break
    if target is None:
        raise RuntimeError('No disposable Yard fixture owns this port')
    labels = target['Config'].get('Labels') or {}
    permitted = [value for key, value in labels.items() if key == 'nox-yard.acceptance' or key.startswith('nox-yard.') and 'smoke' in key]
    if not permitted:
        raise RuntimeError('Refusing to provision Auth against an unlabelled or live Yard')
    mount = next((entry for entry in target['Mounts'] if entry['Destination'] == '/data'), None)
    if mount is None or mount['Type'] not in ('bind', 'volume'):
        raise RuntimeError('Disposable fixture requires its explicit /data mount')
    source = mount.get('Name') if mount['Type'] == 'volume' else mount['Source']
    if source not in _helpers:
        run = docker or _docker
        root = pathlib.Path(__file__).resolve().parents[1]
        root_mount = str(root).replace('\\', '/')
        if root_mount[1:2] == ':':
            root_mount = '/run/desktop/mnt/host/' + root_mount[0].lower() + '/' + root_mount[3:]
        secret = secrets.token_urlsafe(32)
        name = 'nox-yard-auth-fixture-' + secrets.token_hex(6)
        container = run('run', '-d', '--name', name, '--label', 'nox-yard.acceptance=' + permitted[0],
                        '-p', '127.0.0.1::8082', '--workdir', '/src',
                        '--mount', 'type=bind,source=' + root_mount + ',target=/src,readonly',
                        '--mount', 'type=' + mount['Type'] + ',source=' + source + ',target=/data',
                        '--mount', 'type=volume,source=nox-yard-c2-go-cache,target=/go/pkg/mod',
                        '--mount', 'type=volume,source=nox-yard-c2-build-cache,target=/root/.cache/go-build',
                        '--env', 'NOX_AUTH_FIXTURE_SECRET=' + secret,
                        '--env', 'NOX_AUTH_FIXTURE_ORIGIN=' + origin,
                        'golang:1.26-alpine', 'sh', '-ec',
                        'go build -mod=readonly -o /tmp/auth-fixture ./scripts/fixtures/nox-auth && exec /tmp/auth-fixture')
        info = json.loads(_docker('inspect', container))[0]
        port = info['NetworkSettings']['Ports']['8082/tcp'][0]['HostPort']
        endpoint = 'http://127.0.0.1:' + port
        _helpers[source] = {'id': container, 'endpoint': endpoint, 'secret': secret}
        deadline = time.monotonic() + 240
        while time.monotonic() < deadline:
            state = json.loads(_docker('inspect', container))[0]['State']
            if not state['Running']:
                output = subprocess.run(['docker', 'logs', container], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True).stdout
                raise RuntimeError('Auth fixture stopped before readiness: ' + json.dumps({key: state.get(key) for key in ('Status', 'ExitCode', 'OOMKilled', 'Error')}) + '\n' + output[-1800:])
            try:
                with urllib.request.urlopen(endpoint + '/healthz', timeout=2):
                    break
            except OSError:
                time.sleep(.25)
        else:
            raise RuntimeError('Auth fixture did not become ready')
    helper = _helpers[source]
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    request = urllib.request.Request(helper['endpoint'] + '/fixture/session', data=b'',
                                     headers={'X-Fixture-Secret': helper['secret']})
    with opener.open(request, timeout=15) as response:
        session = json.load(response)
    # No raw token is transferred to the tested application through a new API:
    # it finds its own persisted fixture session and validates it via the socket.
    return opener, session
