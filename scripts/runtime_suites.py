"""One runtime-suite inventory for local acceptance and CI."""

SUITES = [
    ("upgrade/password recovery/backup restore", "smoke-upgrade.py", True),
    ("durable jobs and interruption", "smoke-jobs.py", False),
    ("managed updates and rollback", "smoke-managed.py", False),
    ("external/standalone recreation and rollback", "smoke-recreate.py", False),
    ("HTTP/MCP lifecycle, import/adoption and removal", "smoke-mcp.py", False),
    ("automatic project scheduling", "smoke-schedules.py", False),
    ("Yard self-update and SQLite recovery", "smoke-self-update.py", False),
    ("host configuration and self-recreation", "smoke-configuration.py", False),
    ("service-scoped recreation and recovery", "smoke-service-recreate.py", False),
    ("machine permissions and network recovery", "smoke-machine.py", False),
    ("native Compose semantic parity", "smoke-native-parity.py", False),
]
