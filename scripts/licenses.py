#!/usr/bin/env python3
"""Writes THIRD_PARTY_LICENSES.md: every Go module linked into the shipped binaries (cmd/hub, cmd/dth) and
every npm package in the web UI's production bundle, with its license as detected from the package itself.

Run with `make licenses` (needs Go and the web dependencies installed).
"""
import json
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

SPDX = [  # (id, regex over the license text) — checked in order
    ("Apache-2.0", r"Apache License,?\s+Version 2\.0"),
    ("MPL-2.0", r"Mozilla Public License,?\s+(Version|v\.?)\s*2\.0"),
    ("BSD-3-Clause", r"Neither the name of .{0,200} nor the names of its\s+contributors|Redistributions in binary form.{0,400}endorse or promote"),
    ("BSD-2-Clause", r"Redistributions in binary form must reproduce"),
    ("ISC", r"Permission to use, copy, modify, and/or distribute this software for any"),
    ("MIT", r"Permission is hereby granted, free of charge"),
    ("CC0-1.0", r"CC0 1\.0 Universal"),
    ("Unlicense", r"This is free and unencumbered software"),
]


def detect(text: str) -> str:
    flat = " ".join(text.split())
    for spdx, rx in SPDX:
        if re.search(rx, flat, re.S | re.I):
            return spdx
    return "see license file"


def license_file(d: str):
    if not d or not os.path.isdir(d):
        return None
    for name in sorted(os.listdir(d)):
        if re.match(r"(?i)^(license|licence|copying)(\.md|\.txt)?$", name):
            return os.path.join(d, name)
    return None


def go_modules():
    env = dict(os.environ)
    pkgs = subprocess.run(["go", "list", "-deps", "-f", "{{if .Module}}{{.Module.Path}}{{end}}", "./cmd/hub", "./cmd/dth"],
                          cwd=ROOT, env=env, capture_output=True, text=True, check=True).stdout.split()
    mods = sorted(set(p for p in pkgs if p and p != "github.com/GokulMV/DocTheRepo"))
    out = []
    for m in mods:
        info = json.loads(subprocess.run(["go", "list", "-m", "-json", m], cwd=ROOT, env=env, capture_output=True, text=True, check=True).stdout)
        f = license_file(info.get("Dir", ""))
        lic = detect(open(f, encoding="utf-8", errors="replace").read()) if f else "no license file found"
        out.append((m, info.get("Version", ""), lic))
    return out


def npm_packages():
    web = os.path.join(ROOT, "web")
    tree = json.loads(subprocess.run(["npm", "ls", "--omit=dev", "--all", "--json"], cwd=web, capture_output=True, text=True).stdout or "{}")
    seen = {}

    def walk(deps):
        for name, d in (deps or {}).items():
            ver = d.get("version", "")
            if (name, ver) in seen:
                continue
            pj = os.path.join(web, "node_modules", name, "package.json")
            lic = ""
            if os.path.exists(pj):
                meta = json.load(open(pj))
                lic = meta.get("license") or ""
                if isinstance(lic, dict):
                    lic = lic.get("type", "")
            if not lic:
                f = license_file(os.path.join(web, "node_modules", name))
                lic = detect(open(f, encoding="utf-8", errors="replace").read()) if f else "unknown"
            seen[(name, ver)] = lic
            walk(d.get("dependencies"))

    walk(tree.get("dependencies"))
    return sorted((n, v, l) for (n, v), l in seen.items())


def table(rows):
    lines = ["| Package | Version | License |", "|---|---|---|"]
    lines += [f"| {n} | {v} | {l} |" for n, v, l in rows]
    return "\n".join(lines)


def summary(rows):
    counts = {}
    for _, _, l in rows:
        counts[l] = counts.get(l, 0) + 1
    return ", ".join(f"{l}: {c}" for l, c in sorted(counts.items(), key=lambda x: -x[1]))


def main():
    go = go_modules()
    npm = npm_packages()
    doc = f"""# Third-party licenses

DocTheRepo Hub is MIT-licensed (see [LICENSE](LICENSE)). It is built from the open-source packages below, each
under its own license; their license texts ship with the packages (Go module cache, `web/node_modules`). This
file lists what is **linked into the shipped binaries** (`dth-hub`, `dth`) and **bundled into the web UI** —
build and test tools are not included. Regenerate it with `make licenses`.

Licenses are detected from each package's own metadata or license file; check the package when it matters.

## Go modules (`dth-hub`, `dth`)

{summary(go)}

{table(go)}

## Web UI (`web/`, production dependencies)

{summary(npm)}

{table(npm)}

## Other material

| What | Where | License / terms |
|---|---|---|
| Architecture diagrams generated with [archify](https://github.com/tt-a1i/archify) | `docs/architecture/*.html` | The diagram viewer embedded in each file is archify's (MIT, Copyright (c) 2026 tt-a1i and (c) 2025 Cocoon AI), with JetBrains Mono (SIL Open Font License 1.1). The diagram content is this project's. |
| Tree-sitter grammars | loaded at runtime from `DTH_GRAMMARS_DIR` | Each grammar's own license (mostly MIT); not bundled in the binaries. |
| Logo | `docs/brand/` | This project's (MIT), original artwork. |
"""
    path = os.path.join(ROOT, "THIRD_PARTY_LICENSES.md")
    with open(path, "w") as fh:
        fh.write(doc)
    print(f"wrote {path}: {len(go)} Go modules, {len(npm)} npm packages", file=sys.stderr)


if __name__ == "__main__":
    main()
