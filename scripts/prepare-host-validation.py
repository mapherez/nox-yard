#!/usr/bin/env python3
"""Package already-tested ARM64 images and the current source for isolated Pi validation.

Build and test both images from the corresponding source before invoking this
script. It exports local images; it does not publish or deploy them.
"""
import argparse
import datetime
import hashlib
import json
import pathlib
import subprocess
import zipfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--image", required=True)
parser.add_argument("--legacy-image", required=True)
parser.add_argument("--output", default=".tmp/c6-pi5")
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
output = pathlib.Path(args.output).resolve()
if not output.is_relative_to(root / ".tmp") or output == root / ".tmp":
    raise SystemExit("The handoff output must be a new directory inside this repository's .tmp")

def command(*parts):
    return subprocess.check_output(parts, cwd=root, encoding="utf-8").strip()

def checksum(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()

images = []
for reference in (args.image, args.legacy_image):
    item = json.loads(command("docker", "image", "inspect", reference))[0]
    if item["Os"] != "linux" or item["Architecture"] != "arm64":
        raise SystemExit("Pi 5 handoff images must target linux/arm64")
    images.append({"reference": reference, "id": item["Id"], "os": item["Os"], "architecture": item["Architecture"]})
paths = command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z").split("\x00")
text_extensions = {".md", ".txt", ".go", ".mod", ".sum", ".sh", ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".css", ".json", ".yaml", ".yml", ".toml", ".html", ".svg", ".xml", ".example"}
output.mkdir(parents=True, exist_ok=False)
archive = output / "nox-yard-c6-source.zip"
count = 0
with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as packed:
    for relative in sorted(set(paths)):
        if not relative:
            continue
        source = root / relative
        if not source.is_file():
            continue
        if not source.resolve().is_relative_to(root):
            raise SystemExit("Source archive cannot include a symlink outside the repository")
        if source.name.startswith(".env") and source.name != ".env.example":
            continue
        if source.suffix in {".key", ".pem", ".sqlite", ".db"}:
            continue
        content = source.read_bytes()
        if source.suffix in text_extensions or source.name in {"Dockerfile", "LICENSE", ".gitignore", ".gitattributes", ".dockerignore", ".nvmrc"}:
            content = content.replace(b"\r\n", b"\n")
        packed.writestr("nox-yard-c6-source/" + relative, content)
        count += 1
image_archive = output / "nox-yard-c6-pi5.tar"
subprocess.run(["docker", "save", "-o", str(image_archive), args.image, args.legacy_image], cwd=root, check=True)
manifest = {"createdAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "purpose": "isolated Pi 5 validation; not a published release",
    "sourceCommit": command("git", "rev-parse", "HEAD"),
    "workingTreeDirty": bool(command("git", "status", "--porcelain")),
    "baselineTag": "v1.0.1", "baselineCommit": command("git", "rev-parse", "v1.0.1^{commit}"),
    "sourceFiles": count, "images": images,
    "files": {path.name: {"sha256": checksum(path), "bytes": path.stat().st_size} for path in (archive, image_archive)}}
manifest_path = output / "manifest.json"
manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8", newline="")
(output / "SHA256SUMS").write_text("".join(checksum(path) + "  " + path.name + "\n" for path in (archive, image_archive, manifest_path)), encoding="utf-8", newline="")
print("Prepared Pi 5 handoff: " + str(output))
