"""Shared explicit opt-in for disposable MCP fixtures; never use on live Yard."""
import http.cookiejar
import json
import secrets
import urllib.request


def enable_mcp(origin, opener, csrf, token=None):
    def api(path, body=None, method=None):
        req = urllib.request.Request(origin + path, data=None if body is None else json.dumps(body).encode(),
            headers={"Content-Type": "application/json", "Origin": origin, "X-CSRF-Token": csrf}, method=method)
        with opener.open(req, timeout=30) as response:
            return json.load(response)
    if token is None:
        token = api("/api/machine/token", {"confirm": True})["token"]
    settings = api("/api/machine")["settings"]
    settings.update(mcpEnabled=True, authMode="token", write=True, secrets=True)
    api("/api/machine", settings, "PUT")
    return token


def bootstrap_mcp(origin, token=None, docker=None):
    from auth_fixture import login
    opener, setup = login(origin, docker=docker)
    token = enable_mcp(origin, opener, setup["csrfToken"], token)
    return opener, setup, token
