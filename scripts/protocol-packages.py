#!/usr/bin/env python3
"""Build the local protocol artifacts behind `make protocol-packages`."""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys


ROOT = Path(__file__).resolve().parent.parent
SERVER = ROOT / "capabilities" / "server"
OUTPUT = ROOT / ".build" / "protocol"


def run(arguments, **options):
    return subprocess.run(arguments, cwd=SERVER, check=True, **options)


def main():
    go = os.environ.get("GO", "go")
    version = os.environ.get("PROTOCOL_VERSION", "0.1.0")
    if not version or any(character not in "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ.-+" for character in version):
        raise ValueError("PROTOCOL_VERSION must be a plain package version")
    target = json.loads(run([go, "env", "-json", "GOOS", "GOARCH"], capture_output=True, text=True).stdout)
    suffix = ".exe" if target["GOOS"] == "windows" else ""
    binaries = OUTPUT / "bin"
    packages = OUTPUT / "packages"
    artifacts = packages / "artifacts"
    binaries.mkdir(parents=True, exist_ok=True)
    artifacts.mkdir(parents=True, exist_ok=True)

    for command in ("gateway", "driver-simulator", "driver-s7"):
        run([go, "build", "-trimpath", "-buildvcs=false", "-o", str(binaries / (command + suffix)), "./cmd/" + command])

    manifests = []
    for command, identity in (("driver-simulator", "protocol.simulator"), ("driver-s7", "protocol.s7-1200")):
        binary = binaries / (command + suffix)
        reply = run(
            [str(binary)],
            input=json.dumps({"jsonrpc": "2.0", "id": 1, "method": "describe", "params": {}}) + "\n",
            capture_output=True,
            text=True,
            timeout=15,
        )
        message = json.loads(reply.stdout)
        runtime = message.get("result")
        if message.get("jsonrpc") != "2.0" or message.get("id") != 1 or not isinstance(runtime, dict):
            raise ValueError(command + " did not return its driver description")
        if runtime.get("abi") != "driver/v1" or not runtime.get("profiles"):
            raise ValueError(command + " did not describe supported driver/v1 profiles")

        digest = hashlib.sha256()
        with binary.open("rb") as source:
            for block in iter(lambda: source.read(1024 * 1024), b""):
                digest.update(block)
        content = digest.hexdigest()
        artifact = artifacts / content
        shutil.copyfile(binary, artifact)
        artifact.chmod(0o755)
        runtime["targets"] = [{"os": target["GOOS"], "arch": target["GOARCH"], "artifact": content}]
        manifests.append((identity, {
            "id": identity,
            "version": version,
            "title": runtime["profiles"][0]["title"],
            "namespace": identity,
            "runtime": runtime,
        }))

    for identity, manifest in manifests:
        path = packages / (identity + "-" + version + ".json")
        if path.exists() and json.loads(path.read_text()) != manifest:
            raise ValueError(identity + "@" + version + " already has different contents; increase PROTOCOL_VERSION")
        path.write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        print("Built " + identity + "@" + version + " (" + target["GOOS"] + "/" + target["GOARCH"] + ")")
    print("Protocol packages: " + str(packages.relative_to(ROOT)))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print("Protocol package build failed: " + str(error), file=sys.stderr)
        sys.exit(1)
