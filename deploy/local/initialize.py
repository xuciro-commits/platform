#!/usr/bin/env python3
"""Apply reviewed package setup data through the original member submissions API."""
import argparse
import base64
import hashlib
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--package", type=Path, required=True)
    parser.add_argument("--host", required=True)
    parser.add_argument("--tenant", required=True)
    parser.add_argument("--token-env", default="PLATFORM_INITIALIZE_TOKEN")
    parser.add_argument("--run-key", required=True)
    args = parser.parse_args()
    token = os.environ.get(args.token_env)
    if not token or not args.run_key or len(args.run_key) > 128:
        parser.error("an authorized token environment variable and a run key of 1–128 characters are required")
    descriptor = json.loads(args.package.read_text())
    setup = descriptor["blueprint"]["setup"]
    # Payloads are retained JSON strings, so neither numeric values nor their
    # original bytes pass through Python's floating-point JSON representation.
    identity = json.dumps([descriptor["id"], descriptor["version"], setup], ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    digest = hashlib.sha256(identity.encode()).hexdigest()
    headers = {"Authorization": "Bearer " + token, "Platform-Tenant": args.tenant, "Content-Type": "application/json"}

    def request(path, body=None, extra_headers=None):
        raw = None if body is None else json.dumps(body, separators=(",", ":")).encode()
        req = urllib.request.Request(args.host.rstrip("/") + path, raw, headers | (extra_headers or {}))
        try:
            with urllib.request.urlopen(req, timeout=30) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            answer = json.loads(error.read())
            raise RuntimeError(json.dumps(answer, ensure_ascii=False)) from None

    member = request("/v1/me")
    if member["tenantId"] != args.tenant:
        raise RuntimeError("the authenticated member does not belong to the requested tenant")
    actions = {action["schema"]: action for action in request("/v1/actions")}
    run = hashlib.sha256((member["principalId"] + "\n" + args.run_key).encode()).hexdigest()[:24]
    print(f'{descriptor["id"]}@{descriptor["version"]}: setup {digest}, tenant {args.tenant}')
    print("Required roles: " + ", ".join(role["app"] + ":" + role["role"] for role in setup["roles"]))
    for step in setup["steps"]:
        action = actions.get(step["schema"])
        if action is None:
            raise RuntimeError(f'{step["name"]}: the action is unavailable to this member')
        binding = action.get("schemaVersion")
        key = "setup:" + digest + ":" + run + ":" + hashlib.sha256(step["name"].encode()).hexdigest()
        body = {"tenantId": args.tenant, "principalId": member["principalId"], "authority": step["app"], "idempotencyKey": key,
                "schema": {"name": step["schema"], "version": binding["family"] if binding else 1}, "target": step["target"],
                "payload": base64.b64encode(step["payload"].encode()).decode()}
        answer = request("/v1/submissions", body, {"Platform-Schema-Version": binding["version"]} if binding else None)
        if answer.get("error"):
            error = answer["error"]
            raise RuntimeError(f'{step["name"]}: {error.get("code")}: {error.get("message")}')
        if not answer.get("record"):
            raise RuntimeError(f'{step["name"]}: the original request has no accepted receipt')
        print(f'{step["name"]}: {step["schema"]} {step["target"]["id"]} accepted')
    print("Setup complete. Reusing this package, member and run key reuses the original requests.")


if __name__ == "__main__":
    try:
        main()
    except (KeyError, ValueError, OSError, RuntimeError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
