"""Portable UTF-8 fixture writes (including Python 3.9 on macOS)."""

def write_text(path, data, encoding="utf-8", errors=None, newline=None):
    with path.open("w", encoding=encoding, errors=errors, newline=newline) as stream:
        return stream.write(data)

# Keep fixture resource manifests independent of their temporary source trees.
# Never record command arguments: environment values and credentials may occur
# in a command. Only explicit resource identities and safe labels are retained.
def trace_docker(fixture, base, docker_command=None):
    import atexit
    import datetime
    import functools
    import json
    import subprocess

    output = base.parent / (fixture + "-resources.json")
    safe_labels = ("nox-yard.acceptance", "nox-yard.role", "nox-yard.job", "com.docker.compose.project")

    def command(*parts):
        result = subprocess.run([*(docker_command or ["docker"]), *parts], capture_output=True, text=True)
        if result.returncode:
            raise RuntimeError("Cannot inventory fixture resources: " + parts[0])
        return result.stdout

    def inventory():
        result = {}
        for kind, parts in (("containers", ["ps", "-a"]), ("images", ["image", "ls", "--no-trunc"]),
                            ("volumes", ["volume", "ls"]), ("networks", ["network", "ls", "--no-trunc"])):
            entries = [json.loads(line) for line in command(*parts, "--format", "{{json .}}").splitlines()]
            result[kind] = [{key: item[key] for key in ("ID", "Name", "Names", "Repository", "Tag") if key in item} for item in entries]
        cache = subprocess.run([*(docker_command or ["docker"]), "buildx", "du", "--format", "json"], capture_output=True, text=True)
        result["buildCache"] = [{key: item.get(key) for key in ("ID", "CreatedAt")} for item in (json.loads(line) for line in cache.stdout.splitlines())] if cache.returncode == 0 else None
        return result

    manifest = {"fixture": fixture, "startedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                "initialInventory": inventory(), "declared": {"containers": [], "images": [], "volumes": [], "networks": []},
                "labels": {}, "temporaryDirectory": str(base)}

    def persist():
        output.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")

    def declare(kind, value):
        if value and value not in manifest["declared"][kind]:
            manifest["declared"][kind].append(value)

    def finish():
        try:
            manifest["finalInventory"] = inventory()
            manifest["remainingDeclared"] = {}
            for kind, names in manifest["declared"].items():
                before = manifest["initialInventory"][kind]
                after = manifest["finalInventory"][kind]
                def identities(items):
                    values = set()
                    for item in items:
                        values.update(str(item.get(key, "")) for key in ("Name", "Names", "ID"))
                        if "Repository" in item:
                            values.add(item["Repository"] + ":" + item["Tag"])
                    return values
                existing, current = identities(before), identities(after)
                manifest["remainingDeclared"][kind] = [name for name in names if name in current and name not in existing]
            before_cache=manifest["initialInventory"]["buildCache"]
            after_cache=manifest["finalInventory"]["buildCache"]
            manifest["newBuildCacheIDs"] = sorted({item["ID"] for item in after_cache}-{item["ID"] for item in before_cache}) if before_cache is not None and after_cache is not None else None
            manifest["temporaryDirectoryRemains"] = base.exists()
            manifest["finishedAt"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
            persist()
        except Exception as error:
            manifest["inventoryError"] = type(error).__name__
            persist()
    atexit.register(finish)
    persist()

    def decorate(fn):
        @functools.wraps(fn)
        def invoke(*parts, **kwargs):
            parts = [str(value) for value in parts]
            for index, part in enumerate(parts[:-1]):
                value = parts[index + 1]
                if part == "--name":
                    declare("containers", value)
                elif part in ("-t", "--tag") and parts[0] == "build":
                    declare("images", value)
                elif part == "--label":
                    key, _, label = value.partition("=")
                    if key in safe_labels:
                        manifest["labels"][key] = label
                elif part == "--mount" and "type=volume," in value:
                    options = dict(pair.split("=", 1) for pair in value.split(",") if "=" in pair)
                    declare("volumes", options.get("source", ""))
            if parts[:2] == ["volume", "create"]:
                declare("volumes", parts[-1])
            if parts[:2] == ["network", "create"]:
                declare("networks", parts[-1])
            if parts[0] == "tag":
                declare("images", parts[-1])
            persist()
            return fn(*parts, **kwargs)
        return invoke
    return decorate
