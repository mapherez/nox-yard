"""Disposable generic product for Suite lifecycle acceptance; never shipped."""
import base64
import hashlib
import hmac
import http.client
import http.server
import json
import os
from pathlib import Path
import socket
import socketserver

product = os.environ['FIXTURE_PRODUCT_ID']
channel = Path('/run/nox-suite')
state = Path('/fixture-state')
state.mkdir(exist_ok=True)

def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False).encode()

def encoded(value):
    return base64.urlsafe_b64encode(value).decode().rstrip('=')

def declaration():
    version_file = state / 'version'
    return {'contractVersion': '1.0', 'productId': product,
            'productVersion': version_file.read_text().strip() if version_file.exists() else 'fixture-1',
            'displayName': 'Disposable lifecycle participant', 'capabilities': {}}

class Connection(http.client.HTTPConnection):
    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(10)
        self.sock.connect(str(channel / 'nox-backend.sock'))

def call(path, body=None, token=None, key=None):
    connection = Connection('nox-backend')
    headers = {'Content-Type': 'application/json', 'Idempotency-Key': key}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    try:
        connection.request('POST', path, canonical(body) if body else None, headers)
        response = connection.getresponse()
        raw = response.read()
        if response.status not in (200, 201, 204):
            raise RuntimeError('Fixture Backend response: ' + str(response.status))
        return json.loads(raw) if raw else None
    finally:
        connection.close()

def save(path, value):
    temporary = path.with_suffix('.new')
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(value, stream)
    temporary.replace(path)

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_GET(self):
        if self.path != '/nox/integration':
            self.send_error(404)
            return
        body = canonical(declaration())
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        if self.path != '/nox/integration/pairing':
            self.send_error(404)
            return
        try:
            envelope = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
            intent = envelope['intent']
            if envelope['backendOrigin'] != 'http://nox-backend' or intent['productId'] != product or intent['baseUrl'] != 'http://' + product:
                raise ValueError('Unexpected fixture identity')
            receipt_path = state / 'receipt.json'
            receipt = json.loads(receipt_path.read_text()) if receipt_path.exists() else {}
            if receipt.get('pairingId') != intent['pairingId']:
                binding = {'contractVersion': '1.0', 'pairingId': intent['pairingId'], 'nonce': intent['nonce'],
                           'productId': product, 'baseUrl': intent['baseUrl'],
                           'declarationFingerprint': encoded(hashlib.sha256(canonical(declaration())).digest())}
                secret = base64.urlsafe_b64decode(intent['secret'] + '=' * (-len(intent['secret']) % 4))
                proof = {'contractVersion': '1.0', 'binding': binding,
                         'proof': encoded(hmac.new(secret, canonical(binding), hashlib.sha256).digest())}
                result = call('/api/nox/pairings/' + intent['pairingId'] + '/complete', proof, key=intent['pairingId'] + ':complete')
                receipt = {'pairingId': intent['pairingId'], 'credential': result['serviceCredential'], 'acknowledged': False}
                save(receipt_path, receipt)
            if not receipt['acknowledged']:
                call('/api/nox/pairings/' + intent['pairingId'] + '/ack', token=receipt['credential'], key=intent['pairingId'] + ':ack')
                receipt['acknowledged'] = True
                save(receipt_path, receipt)
            self.send_response(204)
            self.end_headers()
        except Exception:
            self.send_error(503, 'Disposable participant could not complete pairing')

class Server(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True

path = channel / (product + '.sock')
# Only used inside the randomly named acceptance volume, with a single owner.
path.unlink(missing_ok=True)
with Server(str(path), Handler) as server:
    os.chmod(path, 0o660)
    server.serve_forever()
