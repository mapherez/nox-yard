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

# Parallel CI groups share this inventory with the unsharded local runner.
# The split balances measured suite durations; every current suite runs once.
CI_GROUPS = {
    "1": ("smoke-recreate.py", "smoke-schedules.py", "smoke-machine.py"),
    "2": ("smoke-managed.py", "smoke-mcp.py", "smoke-self-update.py"),
    "3": ("smoke-jobs.py", "smoke-native-parity.py", "smoke-configuration.py", "smoke-service-recreate.py"),
}

def ci_suites(group):
    selected = [script for scripts in CI_GROUPS.values() for script in scripts]
    current = [script for _, script, legacy in SUITES if not legacy]
    if sorted(selected) != sorted(current) or len(selected) != len(set(selected)):
        raise ValueError("CI groups must partition every current runtime suite exactly once")
    return CI_GROUPS[group]
