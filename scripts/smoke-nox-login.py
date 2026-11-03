#!/usr/bin/env python3
"""Exercise published Auth enrollment and previous-image Yard rollback in isolation.

Does not contact Google or an installed Yard. Full OIDC/session behavior is
covered by HTTP tests; a real Google account remains deployment acceptance.
"""
import argparse
import http.cookiejar
import json
import os
import pathlib
import secrets
import socket
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
from smoke_support import trace_docker, cleanup_fixture

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--image', required=True)
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
name = 'nox-login-smoke-' + secrets.token_hex(6)
base = root / '.tmp' / name
base.mkdir(parents=True)
projects = base / 'projects'
projects.mkdir()
host_projects = str(projects.resolve()).replace('\\', '/')
if host_projects[1:2] == ':':
    host_projects = '/run/desktop/mnt/host/' + host_projects[0].lower() + '/' + host_projects[3:]
volumes = []
containers = []
auth_image = 'ghcr.io/mapherez/nox-auth:v0.0.1'
previous_image = 'ghcr.io/mapherez/nox-yard:v1.3.13'
issuer = 'https://auth.example.test'
password = secrets.token_urlsafe(24)
jar = http.cookiejar.CookieJar()
browser = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

@trace_docker(name, base)
def docker(*parts, env=None):
    result = subprocess.run(['docker', *parts], capture_output=True, text=True, env=env)
    if result.returncode:
        raise RuntimeError('Fixture Docker command failed: ' + ' '.join(parts[:2]) + '\n' + result.stderr)
    if 'compose' in parts and 'was created for project' in result.stderr:
        raise AssertionError('Yard claimed ownership of a pre-existing shared volume')
    return result.stdout.strip()

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, url):
        return None

redirects = urllib.request.build_opener(NoRedirect())

def request(url, body=None, expected=200, opener=None, method=None, csrf=''):
    headers = {'Origin': origin, 'Content-Type': 'application/json', 'X-CSRF-Token': csrf}
    req = urllib.request.Request(url, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
    try:
        response = (opener or browser).open(req, timeout=10)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        content = response.read()
        assert response.status == expected, f'{urllib.parse.urlsplit(url).path}: expected {expected}, got {response.status}'
        return (json.loads(content) if content and response.headers.get('Content-Type', '').startswith('application/json') else content, response.headers)

def ready(url):
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        try:
            result, _ = request(url + '/healthz')
            if result['status'] == 'ok':
                return
        except (OSError, AssertionError):
            pass
        time.sleep(.25)
    raise AssertionError('Disposable service did not become ready')

def run(identifier, image, *parts):
    containers.append(identifier)
    return docker('run', '-d', '--name', identifier, '--label', 'nox-yard.acceptance=' + name, *parts, image)

with socket.socket() as probe:
    probe.bind(('127.0.0.1', 0))
    port = probe.getsockname()[1]
origin = 'http://127.0.0.1:' + str(port)
state, shared, admin, auth_data, other_state = [name + suffix for suffix in ('-yard', '-local', '-admin', '-auth', '-other-yard')]
yard_options = ('-p', f'127.0.0.1:{port}:8080', '-v', state + ':/data', '-e', 'NOX_DATA_DIR=/data',
                '-v', '/var/run/docker.sock:/var/run/docker.sock')
current = name + '-current'
compose_ready = False
compose_environment = dict(os.environ, NOX_PUBLIC_URL=origin, NOX_AUTH_PUBLIC_URL=issuer,
                           NOX_SUITE_LOCAL_VOLUME_NAME=shared, NOX_AUTH_ADMIN_VOLUME_NAME=admin,
                           NOX_BIND_ADDRESS='127.0.0.1', NOX_PORT=str(port),
                           NOX_MCP_BIND_ADDRESS='127.0.0.1', NOX_MCP_PORT='0',
                           NOX_YARD_API_ENABLED='false', NOX_YARD_API_KEY='', NOX_YARD_VERSION='')


def compose(*parts):
    return docker('compose', '--env-file', str(base / '.env'), '--project-directory', str(base),
                  '-p', name, '-f', str(root / 'compose.yaml'), '-f', str(base / 'override.yaml'),
                  *parts, env=compose_environment)

try:
    for image in (auth_image, previous_image):
        docker('pull', image)
    architecture = json.loads(docker('image', 'inspect', args.image))[0]['Architecture']
    for image in (auth_image, previous_image):
        assert json.loads(docker('image', 'inspect', image))[0]['Architecture'] == architecture, 'Use native images'
    for volume in (state, shared, admin, auth_data, other_state):
        labels = ['--label', 'nox-yard.acceptance=' + name]
        if volume == shared:
            labels += ['--label', 'com.docker.compose.project=' + name + '-existing-suite']
        docker('volume', 'create', *labels, volume)
        volumes.append(volume)
    old = name + '-previous'
    run(old, previous_image, *yard_options)
    ready(origin)
    before, _ = request(origin + '/api/setup', {'username': 'owner', 'password': password}, expected=201)
    csrf = before['csrfToken']
    request(origin + '/api/managed/settings', {'projectsBase': host_projects}, method='PUT', csrf=csrf)
    docker('stop', old)

    auth = name + '-auth'
    run(auth, auth_image, '-p', '127.0.0.1::8080', '-v', auth_data + ':/data',
        '-v', shared + ':/run/nox-suite', '-v', admin + ':/run/nox-auth-admin',
        '-e', 'NOX_AUTH_PUBLIC_URL=' + issuer, '-e', 'NOX_AUTH_GOOGLE_CLIENT_ID=fixture-client',
        '-e', 'NOX_AUTH_GOOGLE_CLIENT_SECRET=' + secrets.token_urlsafe(32),
        '-e', 'NOX_AUTH_ALLOWED_EMAILS=owner@example.test')
    auth_port = json.loads(docker('inspect', auth))[0]['NetworkSettings']['Ports']['8080/tcp'][0]['HostPort']
    auth_origin = 'http://127.0.0.1:' + auth_port
    ready(auth_origin)
    for client_state, client_origin in ((other_state, "http://second-yard.example.test"),):
        docker('run', '--rm', '--label', 'nox-yard.acceptance=' + name,
               '-v', client_state + ':/data', '-v', shared + ':/run/nox-suite', '-v', admin + ':/run/nox-auth-admin',
               '-e', 'NOX_DATA_DIR=/data', '-e', 'NOX_PUBLIC_URL=' + client_origin, '-e', 'NOX_AUTH_PUBLIC_URL=' + issuer,
               args.image, 'nox-yard', 'auth-setup')
    # Use the shipped Compose with only fixture isolation overrides. Its real
    # command, dependency, environment and shared-volume declarations are tested.
    (base / '.env').write_text('', encoding='utf-8')
    (base / 'override.yaml').write_text(f'''services:
  auth-setup:
    image: {json.dumps(args.image)}
    labels: {{nox-yard.acceptance: {name}}}
    volumes: ['yard-state:/data']
  nox-yard:
    image: {json.dumps(args.image)}
    container_name: {current}
    labels: {{nox-yard.acceptance: {name}}}
    ports: !override ['127.0.0.1:{port}:8080']
    volumes: ['yard-state:/data']
volumes:
  yard-state:
    name: {state}
    external: true
''', encoding='utf-8')
    compose_ready = True
    compose('up', '-d', '--pull', 'never')
    ready(origin)
    initializer = json.loads(docker('inspect', compose('ps', '-aq', 'auth-setup')))[0]
    assert initializer['State']['Status'] == 'exited' and initializer['State']['ExitCode'] == 0
    compose('run', '--rm', '--no-deps', 'auth-setup')
    print('PASS: shipped Compose automatically enrolls Yard with pre-existing Suite volumes; repeated setup succeeds', flush=True)
    mounts = json.loads(docker('inspect', current))[0]['Mounts']
    assert all(mount['Destination'] != '/run/nox-auth-admin' for mount in mounts), 'Runtime has enrollment authority'
    bootstrap, _ = request(origin + '/api/bootstrap')
    assert bootstrap['loginAvailable'] and not bootstrap['authenticated'], 'Old password session was admitted'
    request(origin + '/api/login', {'username': 'owner', 'password': password}, expected=405)
    _, started = request(origin + '/auth/nox/start?return_to=%2Fprojects', expected=302, opener=redirects)
    authorization = urllib.parse.urlsplit(started['Location'])
    assert authorization.scheme + '://' + authorization.netloc == issuer
    params = urllib.parse.parse_qs(authorization.query)
    assert params['redirect_uri'] == [origin + '/auth/nox/callback']
    assert params['client_id'][0].startswith('nox-yard-'), 'OAuth identity must be unique per Yard installation'
    assert params['code_challenge_method'] == ['S256'] and params['state'] and params['nonce']
    # Contact only the disposable provider, never the configured fixture DNS or Google.
    _, authorize = request(auth_origin + authorization.path + '?' + authorization.query, expected=302, opener=redirects)
    login_url = urllib.parse.urlsplit(authorize['Location'])
    assert login_url.path == '/login'
    _, google = request(auth_origin + login_url.path + '?' + login_url.query, expected=302, opener=redirects)
    google_url = urllib.parse.urlsplit(google['Location'])
    assert google_url.hostname == 'accounts.google.com'
    assert urllib.parse.parse_qs(google_url.query)['redirect_uri'] == [issuer + '/auth/google/callback']
    for path in ('/oauth/token', '/oauth/introspect', '/v1/clients/' + params['client_id'][0], '/v1/logout'):
        try:
            redirects.open(auth_origin + path, timeout=5)
        except urllib.error.HTTPError as error:
            assert error.code >= 400
        else:
            raise AssertionError('Private Auth endpoint exposed through TCP: ' + path)
    print('PASS: second Yard enrollment preserves the first callback; private HTTP Yard reaches Auth/Google; no public enrollment/token routes', flush=True)
    compose('down', '--remove-orphans', '--timeout', '1')
    # Yard must leave the installation's shared channels intact on shutdown.
    shared_info = json.loads(docker('volume', 'inspect', shared))[0]
    assert shared_info['Labels']['com.docker.compose.project'] == name + '-existing-suite'
    docker('volume', 'inspect', admin)
    ready(auth_origin)
    print('PASS: stopping Yard preserves the external volumes and the running Auth service', flush=True)
    docker('start', old)
    ready(origin)
    # Force the previous image to authenticate the original record again.
    jar.clear()
    logged_in, _ = request(origin + '/api/login', {'username': 'owner', 'password': password})
    assert logged_in['authenticated'] and logged_in['username'] == 'owner'
    settings, _ = request(origin + '/api/managed/settings')
    assert settings['projectsBase'] == host_projects, 'Existing configuration changed'
    print('PASS: real previous Yard image reopens the same data, accepts the unchanged local password and preserves settings', flush=True)
finally:
    if compose_ready:
        compose('down', '--remove-orphans', '--timeout', '1')
    for identifier in reversed(containers):
        docker('rm', '-f', identifier)
    for volume in reversed(volumes):
        docker('volume', 'rm', volume)
    cleanup_fixture(base, root, name, args.image)
