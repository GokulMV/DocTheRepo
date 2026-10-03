---
name: supply-chain-deps
description: "Probes vulnerable/outdated/unpinned dependencies, transitive risk, license compliance, SBOM presence, lockfile integrity, and build provenance."
applies-to: [web, api, mobile, cli, library, language:any, framework:any]
---

# Supply Chain and Dependencies

## Purpose

Surface risk inherited from third-party code and the build pipeline that produces the shipped artifact — a vulnerable dependency, an unpinned version that can silently change under you, a typosquatted package, or an unverifiable build — any of which lets an attacker compromise the app without touching its own code.

## Applies-to (detail)

Targets any app that declares external dependencies or has a build/packaging pipeline: web and API backends (npm/pip/Maven/Cargo/Go module manifests), mobile apps (CocoaPods/Gradle/npm for React Native), CLIs and libraries (published package manifests, whose vulnerable transitive deps propagate to every consumer). Applies regardless of language or package ecosystem — probes adapt to whichever manifest/lockfile format (`package-lock.json`, `poetry.lock`, `Cargo.lock`, `go.sum`, `Gemfile.lock`) the surface map records. Does not apply to a zero-dependency app with no external packages and no build pipeline beyond a language's own compiler (note the skip reason explicitly rather than silently matching).

## Probes

- **Vulnerable/outdated dependencies**: run the ecosystem's audit tool (`npm audit`, `pip-audit`, `cargo audit`, `govulncheck`, `bundle audit`) or cross-reference the full dependency tree against OSV/NVD/GHSA advisory databases; flag every dependency with a known CVE, especially any reachable from a code path the app actually exercises (not just declared-but-unused).
- **Unpinned versions**: grep manifests for range operators that allow drift (`^`, `~`, `*`, `latest`, `>=` with no upper bound) on production dependencies, and check whether a lockfile is committed and actually enforced in CI (`npm ci` vs `npm install`, `pip install -r` with hashes vs without) so a fresh install can't silently pull a different version than what was tested.
- **Transitive risk**: walk the full resolved dependency graph (not just direct/first-level deps) and check for known-vulnerable or abandoned packages several levels deep; check whether any single transitive dependency is depended on by an unusually large fraction of the tree (a supply-chain single point of failure) and whether it's actively maintained.
- **License compliance**: enumerate the declared license of every dependency (direct and transitive) and flag copyleft licenses (GPL, AGPL, SSPL) pulled into a proprietary/closed-source codebase, licenses with commercial-use restrictions, and any dependency with no declared license at all.
- **SBOM presence**: check whether a Software Bill of Materials is generated (`cyclonedx`, `syft`, or ecosystem-native equivalent) as part of the build, whether it's kept current (regenerated per release, not stale), and whether it's actually consumable (valid CycloneDX/SPDX format, not just a manifest dump).
- **Lockfile integrity**: verify the lockfile's recorded hashes/integrity fields (`integrity` in `package-lock.json`, `--hash` in `requirements.txt`, `Cargo.lock` checksums) are present and actually verified during install (install fails on a hash mismatch) rather than merely recorded and ignored; check the lockfile is committed to version control and not gitignored.
- **Typosquatting/confusion**: check recently-added dependencies against the legitimate package name for near-miss spelling (`reqeusts` vs `requests`, `python3-dateutil` vs `python-dateutil`), check for dependency-confusion risk (an internal/private package name that also exists as a public package on the public registry with a higher version, which package managers may prefer by default), and verify a private-registry scope/namespace is configured where internal packages exist.
- **Build-tool provenance**: check whether the CI/build pipeline pulls build tools (compilers, bundlers, base Docker images) by pinned digest (`image@sha256:...`) versus a mutable tag (`latest`, `node:20`); check whether build artifacts are signed or attested (SLSA provenance, Sigstore/cosign) and whether that signature is verified before deployment; check for build scripts (`postinstall` hooks, Makefile targets) that fetch and execute remote code at build time without integrity pinning.

## What "safe" looks like

No dependency in the resolved tree — direct or transitive — carries a known CVE reachable from an exercised code path without an explicit, documented risk acceptance. Every production dependency is either exact-pinned or lockfile-pinned, and CI installs strictly from the lockfile (`npm ci`, not `npm install`) so builds are reproducible. The dependency graph has no abandoned (no commits/releases in years, no maintainer response to disclosed CVEs) package on a critical path. License usage is consistent with the project's distribution model — no copyleft-in-proprietary conflicts, no unlicensed dependencies in production. An SBOM is generated per release in a standard format and is current. Lockfile integrity hashes are present and enforced (a tampered/substituted package fails install). No dependency name is a plausible typosquat of a legitimate package, and any internal package name is registered/reserved on the public registry or served exclusively from a scoped private registry that takes priority in resolution. Build tooling is pinned by digest, and released artifacts carry verifiable provenance that's checked before deploy.

## Severity rubric

- `critical`: a dependency with a known, actively-exploited RCE or auth-bypass CVE is reachable from a public-facing code path; a dependency-confusion or typosquat package is actually installed in the current lockfile (not just theoretically possible); build pipeline executes unpinned remote code at build time with no integrity check, on a path that produces the shipped artifact.
- `high`: a known-exploitable CVE exists in a reachable transitive dependency several levels deep; lockfile is present but not enforced in CI (installs can silently drift); no SBOM and no way to answer "are we affected by CVE-X" without a manual audit.
- `medium`: unpinned version ranges on production dependencies with no known current CVE but real drift risk; a copyleft license present in a proprietary codebase without legal review; an abandoned (unmaintained) dependency on a non-critical path.
- `low`: outdated dependency with no known exploitable CVE for this usage; build tooling pinned to a mutable tag rather than a digest, with no evidence of tampering.
- `info`: missing SBOM on an internal-only tool; license metadata missing for a dev-only (non-shipped) dependency.

## Detection method

For vulnerable dependencies, detection is an audit-tool or advisory-database match between a resolved dependency version and a published CVE/GHSA entry, cross-checked against whether the app's code actually imports/calls the vulnerable module or function (reachability, not just presence). For unpinned versions, detection is a manifest range operator combined with either no lockfile or a CI install command that bypasses it. For transitive risk, detection is walking the full resolved graph (`npm ls`, `pip show`/`pipdeptree`, `cargo tree`) and matching each node against the same advisory sources, plus a maintenance-activity check (last release/commit date) on outlier nodes. For license compliance, detection is a license-scanner (`license-checker`, `pip-licenses`, `cargo-license`) output cross-referenced against a declared policy list of disallowed license types. For SBOM, detection is the presence and validity (schema-parseable) of a generated SBOM file matching the current release's dependency set. For lockfile integrity, detection is attempting an install with a deliberately corrupted lockfile hash and confirming it fails rather than silently proceeding. For typosquatting, detection is a string-distance (edit distance ≤2, or homoglyph) comparison of each dependency name against the top packages in its ecosystem, plus a registry lookup confirming whether an internal package name collides with a public one. For build provenance, detection is inspecting the CI config for digest-pinned vs tag-pinned base images/tools, and checking for a provenance attestation file (SLSA predicate, `.sig` from cosign) alongside the release artifact. Evidence for the Finding is the CVE/GHSA ID with affected-version range, the manifest/lockfile diff, the license-scanner output line, or the CI config excerpt showing the unpinned/unverified step.

## References

Skills: `dependency-management-deps-audit`, `security-audit`. Standards/tools: OSV.dev and GitHub Advisory Database (vulnerability data), SLSA framework (build provenance levels), Sigstore/cosign (artifact signing), CycloneDX and SPDX (SBOM formats), OWASP Dependency-Check and OWASP Top 10 (A06:2021 Vulnerable and Outdated Components), npm/PyPI/crates.io official audit tooling documentation.
