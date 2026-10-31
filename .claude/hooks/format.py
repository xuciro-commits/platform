#!/usr/bin/env python3
import json
import os
import subprocess
import sys

path = json.load(sys.stdin).get("tool_input", {}).get("file_path", "")
if path.endswith(".go") and os.path.isfile(path):
    subprocess.run(["gofmt", "-w", path], capture_output=True, timeout=60)
sys.exit(0)
