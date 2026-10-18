#!/usr/bin/env python3
"""ADR-0045's shared HTTP route; invoked by make rehearse, never an executor."""
import argparse
import base64
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]


def require(value, reason):
    if not value:
        raise RuntimeError(reason)


def request(url, token="", tenant="", body=None, headers=None):
    metadata = {"Accept-Language": "en", **(headers or {})}
    if token:
        metadata["Authorization"] = "Bearer " + token
    if tenant:
        metadata["Platform-Tenant"] = tenant
    data = None
    if body is not None:
        metadata["Content-Type"] = "application/json"
        data = json.dumps(body).encode()
    call = urllib.request.Request(url, data=data, headers=metadata)
    try:
        with urllib.request.urlopen(call, timeout=15) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        return json.loads(error.read())


def wait(read, predicate, reason, timeout=40):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        try:
            last = read()
            if predicate(last):
                return last
        except (OSError, KeyError, ValueError):
            pass
        time.sleep(0.2)
    raise RuntimeError(reason + ": " + str(last))


class Member:
    def __init__(self, server, tenant, token):
        self.server, self.tenant, self.token = server, tenant, token
        self.artifacts = set()

    def call(self, path, body=None, headers=None):
        return request(self.server + "/v1/" + path, self.token, self.tenant, body, headers)

    def record(self, typ, identity):
        path = "records/" + urllib.parse.quote(typ, safe="") + "/" + urllib.parse.quote(identity, safe="")
        return self.call(path)

    def draft(self, schema, typ, identity, payload, revision=None):
        action = next((a for a in self.call("actions") if a["schema"] == schema), None)
        require(action and action.get("schemaVersion"), "Action has no content declaration: " + schema)
        binding = action["schemaVersion"]
        body = {"tenantId": self.tenant, "principalId": self.call("me")["principalId"],
                "authority": schema.split(".")[0], "idempotencyKey": str(uuid.uuid4()),
                "schema": {"name": schema, "version": binding["family"]},
                "target": {"type": typ, "id": identity},
                "payload": base64.b64encode(json.dumps(payload).encode()).decode()}
        if revision is not None:
            body["expectedRevision"] = revision
        return {"submission": body, "version": binding["version"]}

    def send(self, draft):
        return self.call("submissions", draft["submission"], {"Platform-Schema-Version": draft["version"]})

    def decide(self, schema, typ, identity, payload, revision=None):
        answer = self.send(self.draft(schema, typ, identity, payload, revision))
        require("record" in answer, schema + ": " + str(answer.get("error")))
        return answer["record"]

    def candidate(self):
        selection = {"drafts": [{"kind": "object", "id": "MIGRATION-O"}, {"kind": "flow", "id": "MIGRATION-P"}]}
        preview = self.call("releases/preview", selection)
        require(preview.get("candidateId") and not preview.get("diagnostic"), "Candidate: " + str(preview))
        self.artifacts.add("releases/" + self.tenant + "/" + preview["candidateId"].removeprefix("sha256-v1:").replace(":", "-") + ".json")
        saved = self.call("releases/candidates", {**selection, "candidateId": preview["candidateId"], "key": str(uuid.uuid4())})
        require(saved.get("id") == preview["candidateId"], "Candidate save failed")
        return saved["id"]

    def activate(self, candidate):
        answer = self.call("releases/active", {"candidateId": candidate, "key": str(uuid.uuid4())})
        require(answer.get("id") == candidate, "Activation: " + str(answer))


def prepare(builder, operator, suffix):
    typ, flow = "build.migration" + suffix, "build.migrationreview" + suffix
    fields = [{"name": "note", "title": "Note", "type": "text"}]
    actions = [{"name": "close", "title": "Close", "from": ["open"], "to": "done"},
               {"name": "reject", "title": "Reject", "from": ["open"], "to": "rejected"}]
    steps = [{"name": "ask", "title": "Confirm", "kind": "ask", "ask": "user", "answers": ["yes"], "next": "write"},
             {"name": "write", "kind": "action", "act": "edit", "inputs": {"note": {"source": "literal", "value": "Old flow completed"}}, "next": "close"},
             {"name": "close", "kind": "action", "act": "close"}]
    builder.decide("build.object.create", "build.object", "MIGRATION-O", {
        "name": typ[6:], "title": "Migration recovery records", "fields": fields,
        "states": [{"name": name, "title": name.title()} for name in ["open", "done", "rejected"]], "actions": actions})
    builder.decide("build.object.publish", "build.object", "MIGRATION-O", {})
    builder.decide("build.process.create", "build.process", "MIGRATION-P", {
        "name": flow[6:], "title": "Migration review", "object": typ, "when": "open", "steps": steps})
    builder.decide("build.process.publish", "build.process", "MIGRATION-P", {})
    source = builder.candidate()
    builder.activate(source)
    frozen = builder.call("releases/candidates/" + source)["assets"]
    history = {}
    for identity in ["A", "B", "C"]:
        operator.decide(typ + ".create", typ, identity, {"note": "Original " + identity})
        wait(lambda: builder.record("flow.instance", flow + ":" + identity),
             lambda row: row.get("record", {}).get("state") == "waiting", "Original flow did not wait")
        history[identity] = operator.record(typ, identity)["history"]

    # A new write retains the old declaration and is refused after activation.
    stale = operator.draft(typ + ".create", typ, "STALE", {"note": "Old client"})
    fields = [{**fields[0], "readOnly": True, "aliasOf": "label"},
              {"name": "label", "title": "Label", "type": "text"},
              {"name": "enabled", "title": "Enabled", "type": "boolean", "required": True}]
    builder.decide("build.object.edit", "build.object", "MIGRATION-O", {"fields": fields, "actions": actions[1:]})
    steps[1] = {**steps[1], "inputs": {"label": {"source": "literal", "value": "New flow completed"}}}
    steps[2] = {"name": "close", "kind": "action", "act": "reject"}
    builder.decide("build.process.edit", "build.process", "MIGRATION-P", {"steps": steps})
    target = builder.candidate()
    plan = {"from": source, "to": target, "scope": {"type": typ}, "steps": [
        {"kind": "rename", "types": [typ], "batch": 2, "fields": [{"from": "note", "to": "label"}]},
        {"kind": "add", "types": [typ], "batch": 2, "fields": [{"to": "enabled", "expr": {"kind": "boolean", "op": "", "lit": {"value": True}}}]},
        {"kind": "retire-action", "types": [typ], "batch": 2, "action": typ + ".close"},
    ]}
    builder.decide("build.migration-plan.create", "build.migration-plan", "MIGRATION-PLAN", {"name": "migration" + suffix, "title": "Retained migration", "plan": plan})
    builder.decide("build.migration-plan.publish", "build.migration-plan", "MIGRATION-PLAN", {})
    published = builder.record("build.migration-plan", "MIGRATION-PLAN")["record"]
    sealed_plan = json.loads(published["published"])
    asset = {"ref": sealed_plan["ref"], "sourceVersion": sealed_plan["sourceVersion"]}
    review = builder.call("releases/migrations/review", plan)
    require(review.get("id") and not review.get("issues"), "Migration review: " + str(review))
    builder.decide("build.migration.start", "build.migration", "MIGRATION-RUN", {"asset": asset, "review": review["id"], "reviewedAt": review["at"]})
    original = builder.draft("build.migration.advance", "build.migration", "MIGRATION-RUN", {})
    receipt = builder.send(original)
    require("record" in receipt, "First migration batch failed: " + str(receipt))
    progress = builder.record("build.migration", "MIGRATION-RUN")
    require(progress["record"]["migrated"] == 2, "First batch did not migrate exactly two rows")
    # Ordinary writes stay frozen while the plan is incomplete.
    denied = operator.send(operator.draft(typ + ".edit", typ, "C", {"note": "Should stay frozen"}))
    require(denied.get("error", {}).get("code") == "ERROR_CODE_CONFLICT", "Migration did not freeze source writes")
    print("ok   " + suffix + ": sealed plan, three waiting flows, first bounded batch, writes frozen", flush=True)
    return {"type": typ, "flow": flow, "source": source, "target": target, "frozen": frozen,
            "history": history, "original": original, "receipt": receipt, "progress": progress, "stale": stale}


def state(builder, saved):
    paths = [("build.migration", "MIGRATION-RUN"), ("build.migration-plan", "MIGRATION-PLAN"),
             ("build.object", "MIGRATION-O"), ("build.process", "MIGRATION-P")]
    paths += [(saved["type"], identity) for identity in ["A", "B", "C"]]
    paths += [("flow.instance", saved["flow"] + ":" + identity) for identity in ["A", "B", "C"]]
    return [builder.record(typ, identity) for typ, identity in paths] + [builder.call("releases/active")]


def retry(builder, saved):
    require(builder.record("build.migration", "MIGRATION-RUN") == saved["progress"], "Restart changed the saved batch progress")
    require(builder.send(saved["original"]) == saved["receipt"], "Same-key retry changed its original receipt")
    require(builder.record("build.migration", "MIGRATION-RUN") == saved["progress"], "Retry performed a second migration batch")


def resume(builder, operator, saved, suffix):
    retry(builder, saved)
    for _ in range(20):
        run = builder.record("build.migration", "MIGRATION-RUN")["record"]
        if run["state"] == "complete":
            break
        require(run["state"] == "running" and not run.get("failures"), "Migration stopped: " + str(run))
        builder.decide("build.migration.advance", "build.migration", "MIGRATION-RUN", {}, run["revision"])
    else:
        raise RuntimeError("Migration did not finish within its declared batches")
    builder.activate(saved["target"])
    require(builder.call("releases/candidates/" + saved["source"])["assets"] == saved["frozen"], "Migration rewrote the frozen source")
    refusal = operator.send(saved["stale"])
    require(refusal.get("error", {}).get("code") == "ERROR_CODE_UNKNOWN_SCHEMA" and "refresh" in refusal["error"].get("message", ""), "Old family executed current rules")
    require("record" not in operator.record(saved["type"], "STALE"), "Refused stale request wrote a row")
    for identity in ["A", "B", "C"]:
        row = operator.record(saved["type"], identity)
        require(row["record"]["enabled"] is True and row["record"]["label"] == "Original " + identity, "Target values did not migrate")
        require(row["history"][-len(saved["history"][identity]):] == saved["history"][identity], "Migration rewrote historical decisions")
        instance = builder.record("flow.instance", saved["flow"] + ":" + identity)["record"]
        require(instance["release"] == saved["source"], "Migration rebound an in-flight flow")
        task = next(token["task"] for token in instance["tokens"] if token.get("waits") == "ask")
        operator.decide("work.task.complete", "work.task", task, {"answer": "yes"})
        wait(lambda: operator.record(saved["type"], identity), lambda result: result.get("record", {}).get("state") == "done", "Old flow did not execute its retired action")
        require(operator.record(saved["type"], identity)["record"]["label"] == "Old flow completed", "Old field write was not mapped to the migrated value")
    operator.decide(saved["type"] + ".create", saved["type"], "NEW", {"label": "New client", "enabled": True})
    instance = wait(lambda: builder.record("flow.instance", saved["flow"] + ":NEW"),
                    lambda result: result.get("record", {}).get("state") == "waiting", "New flow did not wait")["record"]
    require(instance["release"] == saved["target"], "New instance used an earlier release")
    task = next(token["task"] for token in instance["tokens"] if token.get("waits") == "ask")
    operator.decide("work.task.complete", "work.task", task, {"answer": "yes"})
    wait(lambda: operator.record(saved["type"], "NEW"), lambda result: result.get("record", {}).get("state") == "rejected", "New flow used the retired action")
    print("ok   " + suffix + ": original retry, resumed migration, target activation, old/new version execution", flush=True)


def oidc_token(user, issuer):
    data = urllib.parse.urlencode({"grant_type": "password", "client_id": "platform-cli",
                                  "client_secret": "cliLocalOnly" + "0" * 52, "username": user, "password": "Plant-Local-1"}).encode()
    with urllib.request.urlopen(issuer.rstrip("/") + "/oidc/token", data=data, timeout=15) as response:
        return json.load(response)["access_token"]


def native():
    require(not os.environ.get("PLATFORM_DEV_LIGHT"), "Migration rehearsal requires persistent infrastructure")
    compose = ["docker", "compose", "-f", str(ROOT / "deploy/local/compose.yaml")]
    container = subprocess.check_output(compose + ["ps", "-q", "postgres"], cwd=ROOT, text=True).strip()
    require(container, "Existing PostgreSQL infrastructure is not running")
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    for solution in ["hospitality", "manufacturing"]:
        binary = ROOT / ".build/dev" / (solution + "-server")
        require(binary.exists(), "Start the local environment first")
        metadata = subprocess.check_output(["go", "version", "-m", str(binary)], text=True)
        require("vcs.revision=" + revision in metadata, "Local binary differs from HEAD: " + solution)
    nonce = str(time.time_ns())
    databases = ["migration_rehearsal_" + nonce, "migration_restore_" + nonce]
    created, processes, streams = [], [], []
    pg = ["docker", "exec", "-i", container]
    pgport = os.environ.get("PG_PORT", "5433")
    issuer = "http://localhost:" + os.environ.get("IDP_PORT", "8480") + "/auth/v1/"
    s3 = "http://127.0.0.1:" + os.environ.get("FILES_PORT", "9000") + "/platform"
    signing_key = secrets.token_hex(32)
    configurations = []
    try:
        for database in databases:
            subprocess.run(pg + ["createdb", "-U", "platform", database], check=True, stdout=subprocess.DEVNULL)
            created.append(database)
        with tempfile.TemporaryDirectory(prefix="migration-rehearsal-", dir=ROOT / ".build") as temporary:
            directory = Path(temporary)
            for solution, suffix, builder_user, operator_user, builder_id, operator_id in [
                ("hospitality", "hotel", "manager@hotel.test", "desk@hotel.test", "manager-1", "desk-1"),
                ("manufacturing", "plant", "sup@plant.test", "op1@plant.test", "sup-1", "op-l1"),
            ]:
                tenant = "migration-" + suffix + "-" + nonce[-10:]
                roles = {"build": "builder", "platform": "admin", "work": "admin", "flow": "admin", "core": "steward"}
                if suffix == "plant":
                    roles.update({"erp": "controller", "mes": "supervisor"})
                else:
                    roles.update({"crm": "sales-manager", "pms": "manager"})
                spec = [{"id": tenant, "seats": [
                    {"subjects": ["user:" + builder_user], "id": builder_id, "roles": roles},
                    {"subjects": ["user:" + operator_user], "id": operator_id, "roles": {"build": "user", "work": "worker"}},
                ]}]
                spec_path = directory / (suffix + ".json")
                spec_path.write_text(json.dumps(spec))
                with socket.socket() as listener:
                    listener.bind(("127.0.0.1", 0))
                    port = listener.getsockname()[1]
                configurations.append({"solution": solution, "suffix": suffix, "tenant": tenant, "spec": spec_path, "port": port,
                                       "builder": Member("http://127.0.0.1:" + str(port), tenant, oidc_token(builder_user, issuer)),
                                       "operator": Member("http://127.0.0.1:" + str(port), tenant, oidc_token(operator_user, issuer))})

            def stop():
                for process in processes:
                    if process.poll() is None:
                        process.send_signal(signal.SIGINT)
                for process in processes:
                    process.wait(timeout=20)
                processes.clear()
                for stream in streams:
                    stream.close()
                streams.clear()

            def start(database, snapshot_every=10000):
                for config in configurations:
                    log = (directory / (config["suffix"] + ".log")).open("a")
                    streams.append(log)
                    environment = {**os.environ, "PLATFORM_S3_ACCESS_KEY": "platform", "PLATFORM_S3_SECRET_KEY": "platform-files-local-only",
                                   "PLATFORM_PERSONAL_TOKEN_KEY": signing_key, "PLATFORM_SECRET_SINK": "sinkLocalOnly" + "0" * 51}
                    command = [str(ROOT / ".build/dev" / (config["solution"] + "-server")), "-addr", "127.0.0.1:" + str(config["port"]),
                               "-database", "postgres://platform:platform-local-only@127.0.0.1:" + pgport + "/" + database,
                               "-oidc-issuer", issuer, "-oidc-keys", issuer + "oidc/certs", "-files", s3,
                               "-tenants", str(config["spec"]), "-snapshot-every", str(snapshot_every)]
                    process = subprocess.Popen(command, cwd=ROOT, env=environment, stdout=log, stderr=subprocess.STDOUT)
                    processes.append(process)
                    def health():
                        if process.poll() is not None:
                            tail = (directory / (config["suffix"] + ".log")).read_text().splitlines()[-5:]
                            raise RuntimeError("Rehearsal host exited: " + "\n".join(tail))
                        return request(config["builder"].server + "/healthz")
                    wait(health,
                         lambda health: health.get("status") == "ok" and health.get("quarantined") == 0,
                         "Persistent " + config["suffix"] + " host failed to start")
                    require(process.poll() is None, "Rehearsal host exited")

            start(databases[0])
            for config in configurations:
                config["saved"] = prepare(config["builder"], config["operator"], config["suffix"])
                config["before"] = state(config["builder"], config["saved"])
            stop()
            start(databases[0])
            for config in configurations:
                require("from the snapshot at" in (directory / (config["suffix"] + ".log")).read_text(), "Restart did not use its persisted snapshot")
                require(state(config["builder"], config["saved"]) == config["before"], "Snapshot restart changed paused migration state")
            stop()
            dump = subprocess.check_output(pg + ["pg_dump", "-U", "platform", "-Fc", databases[0]])
            subprocess.run(pg + ["pg_restore", "-U", "platform", "-d", databases[1], "--no-owner"], input=dump, check=True)
            start(databases[1])
            for config in configurations:
                require(state(config["builder"], config["saved"]) == config["before"], "Backup restore changed paused migration state")
            stop()
            start(databases[1], snapshot_every=0)
            for config in configurations:
                require(state(config["builder"], config["saved"]) == config["before"], "Full journal rebuild changed paused migration state")
                resume(config["builder"], config["operator"], config["saved"], config["suffix"])
                config["after"] = state(config["builder"], config["saved"])
            stop()
            start(databases[1], snapshot_every=0)
            for config in configurations:
                require(state(config["builder"], config["saved"]) == config["after"], "Recovery changed activated migration or completed flows")
            stop()
            test_environment = {**os.environ, "PLATFORM_TEST_DATABASE": "postgres://platform:platform-local-only@127.0.0.1:" + pgport + "/" + databases[0]}
            subprocess.run(["make", "test-go", "PKG=. ./platform", "RUN=TestMigration|TestCustomerExtension|TestSubmissionSchema"], cwd=ROOT, env=test_environment, check=True)
            print("ok   both industries: PostgreSQL snapshot, backup restore, full replay, migration recovery and old/new flows", flush=True)
    finally:
        for process in processes:
            if process.poll() is None:
                process.send_signal(signal.SIGINT)
                try:
                    process.wait(timeout=20)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
        for stream in streams:
            stream.close()
        keys = sorted({key for config in configurations for key in config["builder"].artifacts})
        if keys:
            # Use the original file owner and its S3 SDK; no second signer or
            # storage adapter. The tiny process only deletes this run's keys.
            with tempfile.TemporaryDirectory(prefix="migration-cleanup-", dir=ROOT / ".build") as temporary:
                helper = Path(temporary) / "main.go"
                helper.write_text('''package main
import ("context"; "encoding/json"; "log"; "os"; "strings"; "platformserver")
func main() {
 ctx:=context.Background()
 files,err:=platformserver.NewS3Files(ctx,os.Args[1]);if err!=nil {log.Fatal(err)}
 var keys []string;if err:=json.NewDecoder(os.Stdin).Decode(&keys);err!=nil {log.Fatal(err)}
 for _,key:=range keys {
  if !strings.HasPrefix(key,"releases/migration-") || strings.Contains(key,"..") {log.Fatal("foreign rehearsal key")}
  if err:=files.Delete(ctx,key);err!=nil {log.Fatal(err)}
 }
}
''')
                environment = {**os.environ, "PLATFORM_S3_ACCESS_KEY": "platform", "PLATFORM_S3_SECRET_KEY": "platform-files-local-only"}
                subprocess.run(["go", "run", str(helper), s3], input=json.dumps(keys).encode(),
                               cwd=ROOT / "capabilities/server", env=environment, check=True)
        for database in reversed(created):
            subprocess.run(pg + ["dropdb", "-U", "platform", "--force", database], check=True, stdout=subprocess.DEVNULL)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--phase", choices=["prepare", "retry", "resume", "verify", "state"])
    parser.add_argument("--saved", type=Path)
    args = parser.parse_args()
    if args.phase is None:
        native()
        return
    require(args.saved, "A retained scenario state is required")
    builder = Member(os.environ["SERVER"], os.environ["TENANT"], os.environ["MIGRATION_BUILDER_TOKEN"])
    operator = Member(os.environ["SERVER"], os.environ["TENANT"], os.environ["MIGRATION_OPERATOR_TOKEN"])
    suffix = os.environ["MIGRATION_SUFFIX"]
    if args.phase == "prepare":
        saved = prepare(builder, operator, suffix)
        args.saved.write_text(json.dumps(saved))
    else:
        saved = json.loads(args.saved.read_text())
        if args.phase == "retry":
            retry(builder, saved)
        elif args.phase == "resume":
            resume(builder, operator, saved, suffix)
            saved["after"] = state(builder, saved)
            args.saved.write_text(json.dumps(saved))
        elif args.phase == "state":
            print(json.dumps(state(builder, saved)))
        else:
            require(state(builder, saved) == saved["after"], "Persistent migration state changed after recovery")


if __name__ == "__main__":
    main()
