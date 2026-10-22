#!/usr/bin/env python3
"""Offline build-boundary regressions; no Docker daemon or compiler required."""
import hashlib
import functools
import io
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


def run(args, **kwargs):
    return subprocess.run(args, text=True, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, **kwargs)


@functools.lru_cache(maxsize=1)
def workflows():
    paths = sorted((ROOT / ".github/workflows").glob("*.yml"))
    result = run(["go", "run", "-mod=vendor", str(ROOT / "scripts/tests/parse_workflows.go"),
                  *map(str, paths)], cwd=ROOT,
                 env=dict(os.environ, GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off"))
    if result.returncode:
        raise RuntimeError("Go 1.27.3 and synchronized vendor tree required for YAML policy checks: " + result.stderr)
    return json.loads(result.stdout)


class DockerBoundary(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "checkout"
        self.repo.mkdir()
        self.tools = self.root / "tools"
        self.tools.mkdir()
        self.log = self.root / "docker.jsonl"
        stub = self.tools / "docker"
        stub.write_text("""#!/usr/bin/env python3
import json, os, pathlib, sys
args = sys.argv[1:]
record = {'args': args}
if args[0] == 'build':
    context = pathlib.Path(args[-1])
    record['files'] = sorted(str(p.relative_to(context)) for p in context.rglob('*') if p.is_file())
else:
    for i, arg in enumerate(args):
        if arg == '--mount':
            parts = dict(item.split('=', 1) for item in args[i+1].split(',') if '=' in item)
            if parts.get('dst') == '/workspace':
                source = pathlib.Path(parts['src'])
                record['source'] = str(source)
                record['files'] = sorted(str(p.relative_to(source)) for p in source.rglob('*') if p.is_file())
                record['changed'] = (source / 'cmd/main.go').read_text()
with open(os.environ['DOCKER_TEST_LOG'], 'a') as f:
    f.write(json.dumps(record) + '\\n')
""")
        stub.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.tools) + os.pathsep + os.environ["PATH"],
                        WORKSPACE_DIR=str(self.repo), DOCKER_TEST_LOG=str(self.log))
        run(["git", "init", "-q", str(self.repo)], check=True)
        for name in ("Dockerfile.build", "scripts/docker-builder-entrypoint.sh",
                     "scripts/zig-ar.sh", "scripts/zig-ranlib.sh"):
            dest = self.repo / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, dest)
        self.write("cmd/main.go", "package main\n")
        self.write("go.mod", "module example\ngo 1.27.3\n")
        self.write("Makefile", "all:\n\ttrue\n")
        self.write(".gitignore", "ignored.go\n")
        self.write("internal/deleted/source.go", "package deleted\n")
        run(["git", "-C", str(self.repo), "add", "."], check=True)
        shutil.rmtree(self.repo / "internal/deleted")
        self.write("cmd/main.go", "package main // working tree edit\n")
        self.write("internal/new.go", "package new\n")
        self.write("config/credentials_security_test.go", "package config\n")
        self.write("internal/securefile/security_test.go", "package securefile\n")
        self.write("internal/ignored.go", "ignored\n")
        for name in (".env", ".env.local", "beatportdl-config.yml", "beatportdl-credentials.json",
                     "beatportdl-err.log", "internal/beatportdl-err.log.1", ".third_party/key",
                     "internal/.env", "config/credentials.json", "config/config.yml"):
            self.write(name, "secret\n")
            run(["git", "-C", str(self.repo), "add", "-f", name], check=True)

    def write(self, name, contents):
        path = self.repo / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(contents)

    def invoke(self):
        return run(["sh", str(ROOT / "scripts/docker-build.sh"), "pure-test"],
                   cwd=self.repo, env=self.env)

    def test_snapshot_and_container_boundary(self):
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        records = [json.loads(line) for line in self.log.read_text().splitlines()]
        self.assertEqual(records[0]["files"], ["Dockerfile.build", "scripts/docker-builder-entrypoint.sh",
                                             "scripts/zig-ar.sh", "scripts/zig-ranlib.sh"])
        record = records[-1]
        self.assertEqual(record.get("files"), ["Makefile", "cmd/main.go", "config/credentials_security_test.go",
                                              "go.mod", "internal/new.go", "internal/securefile/security_test.go"])
        self.assertIn("working tree edit", record["changed"])
        self.assertNotEqual(record["source"], str(self.repo))
        args = record["args"]
        for flag in ("--network=none", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--read-only"):
            self.assertIn(flag, args)
        self.assertIn("--user", args)
        mounts = [args[i+1] for i, arg in enumerate(args) if arg == "--mount"]
        self.assertTrue(any("dst=/workspace,readonly" in m for m in mounts))
        self.assertTrue(any("dst=/output" in m and "readonly" not in m for m in mounts))
        self.assertFalse(Path(record["source"]).exists(), "temporary source must be cleaned up")

    def test_source_symlink_cannot_escape_snapshot(self):
        (self.repo / "internal/escape.go").symlink_to(self.repo / ".env")
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("symlink", result.stderr.lower())


class NativeLinkPolicy(unittest.TestCase):
    def test_darwin_dependency_libraries_precede_sdk_libraries(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tools = root / "tools"
            tools.mkdir()
            log = root / "go-build.json"
            stub = tools / "go"
            stub.write_text("#!" + sys.executable + "\n" + """
import json, os, pathlib, sys
if sys.argv[1] == 'build':
    pathlib.Path(os.environ['GO_TEST_LOG']).write_text(json.dumps({
        'flags': os.environ['CGO_LDFLAGS'], 'goos': os.environ['GOOS'],
        'goarch': os.environ['GOARCH'], 'sdk': os.environ['SDKROOT'],
    }))
""")
            stub.chmod(0o755)
            prefix, sdk = root / "deps", root / "sdk"
            env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"],
                       BUILD_DIR=str(root / "output"), MACOS_SDK_PATH=str(sdk), GO_TEST_LOG=str(log))
            for arch in ("arm64", "amd64"):
                with self.subTest(arch=arch):
                    result = run(["bash", str(ROOT / "scripts/build-release-target.sh"),
                                  "darwin-" + arch, str(prefix)], env=env)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    build = json.loads(log.read_text())
                    flags = shlex.split(build["flags"])
                    self.assertEqual((build["goos"], build["goarch"], build["sdk"]), ("darwin", arch, str(sdk)))
                    self.assertLess(flags.index("-L" + str(prefix / "lib")),
                                    flags.index("-L" + str(sdk / "usr/lib")),
                                    "pinned zlib must take precedence over SDK zlib")


class NativeCache(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.cache = self.root / "cache"
        downloads = self.cache / "downloads"
        downloads.mkdir(parents=True)
        tarball = downloads / "zlib-1.3.2.tar.gz"
        with tarfile.open(tarball, "w:gz") as archive:
            data = b"verified zlib\n"
            info = tarfile.TarInfo("zlib-1.3.2/source.c")
            info.size = len(data)
            archive.addfile(info, io.BytesIO(data))
        self.taglib = self.cache / "sources/taglib-fixture"
        self.taglib.mkdir(parents=True)
        run(["git", "init", "-q", str(self.taglib)], check=True)
        (self.taglib / "source.cpp").write_text("verified taglib\n")
        (self.taglib / ".gitignore").write_text("ignored.cpp\n")
        run(["git", "-C", str(self.taglib), "add", "."], check=True)
        run(["git", "-C", str(self.taglib), "-c", "user.name=Test", "-c", "user.email=test@example.invalid",
             "commit", "-qm", "fixture"], check=True)
        commit = run(["git", "-C", str(self.taglib), "rev-parse", "HEAD"], check=True).stdout.strip()
        tools = self.root / "tools"
        tools.mkdir()
        self.log = self.root / "cmake.log"
        stub = tools / "cmake"
        stub.write_text("""#!/usr/bin/env python3
import os, pathlib, sys
args = sys.argv[1:]
if '-S' in args:
    source = pathlib.Path(args[args.index('-S')+1])
    data = (source / ('source.c' if (source / 'source.c').exists() else 'source.cpp')).read_text()
    with open(os.environ['CMAKE_TEST_LOG'], 'a') as log: log.write(data)
""")
        stub.chmod(0o755)
        self.env = dict(os.environ, PATH=str(tools)+os.pathsep+os.environ["PATH"],
                        THIRD_PARTY_ROOT=str(self.cache), TAGLIB_VERSION="fixture", TAGLIB_COMMIT=commit,
                        ZLIB_SHA256=hashlib.sha256(tarball.read_bytes()).hexdigest(),
                        CMAKE_TEST_LOG=str(self.log))

    def invoke(self):
        return run(["bash", str(ROOT / "scripts/build-release-deps.sh"), "darwin-arm64",
                    str(self.root / "prefix")], env=self.env)

    def test_zlib_extracted_tree_is_never_trusted(self):
        source = self.cache / "sources/zlib-1.3.2"
        source.mkdir()
        (source / "source.c").write_text("tampered zlib\n")
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.log.read_text(), "verified zlib\nverified taglib\n")

    def test_taglib_dirty_tracked_untracked_and_ignored_sources_rejected(self):
        for name in ("source.cpp", "injected.cpp", "ignored.cpp"):
            with self.subTest(name=name):
                path = self.taglib / name
                original = path.read_bytes() if path.exists() else None
                path.write_text("tampered\n")
                result = self.invoke()
                self.assertNotEqual(result.returncode, 0, "tampered TagLib reached cmake")
                self.assertIn("pristine", result.stderr.lower())
                self.assertFalse(self.log.exists())
                if original is None:
                    path.unlink()
                else:
                    path.write_bytes(original)

    def test_tampered_download_rejected(self):
        (self.cache / "downloads/zlib-1.3.2.tar.gz").write_bytes(b"tampered")
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("sha256 mismatch", result.stderr)
        self.assertFalse(self.log.exists())

    def test_taglib_index_flags_cannot_hide_modified_source(self):
        run(["git", "-C", str(self.taglib), "update-index", "--assume-unchanged", "source.cpp"], check=True)
        (self.taglib / "source.cpp").write_text("hidden tampering\n")
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0, "index flags hid source tampering")
        self.assertIn("pristine", result.stderr.lower())
        self.assertFalse(self.log.exists())

    def test_taglib_replacement_objects_cannot_substitute_pinned_tree(self):
        pinned = self.env["TAGLIB_COMMIT"]
        (self.taglib / "source.cpp").write_text("replacement-controlled taglib\n")
        run(["git", "-C", str(self.taglib), "add", "source.cpp"], check=True)
        run(["git", "-C", str(self.taglib), "-c", "user.name=Test", "-c", "user.email=test@example.invalid",
             "commit", "-qm", "altered tree"], check=True)
        altered = run(["git", "-C", str(self.taglib), "rev-parse", "HEAD"], check=True).stdout.strip()
        for replacement_base in ("refs/replace/", "refs/cache-replacements/"):
            with self.subTest(replacement_base=replacement_base):
                env = dict(self.env, GIT_REPLACE_REF_BASE=replacement_base)
                env.pop("GIT_NO_REPLACE_OBJECTS", None)
                run(["git", "-C", str(self.taglib), "replace", pinned, altered], env=env, check=True)
                run(["git", "-C", str(self.taglib), "update-ref", "HEAD", pinned], env=env, check=True)
                self.assertEqual(run(["git", "-C", str(self.taglib), "rev-parse", "HEAD"], env=env,
                                     check=True).stdout.strip(), pinned)
                self.assertEqual(run(["git", "-C", str(self.taglib), "status", "--porcelain"], env=env,
                                     check=True).stdout, "")
                self.env = env
                result = self.invoke()
                self.assertNotEqual(result.returncode, 0, "replacement-controlled source reached CMake")
                self.assertIn("pristine", result.stderr.lower())
                self.assertFalse(self.log.exists())

    def test_taglib_submodule_untracked_source_rejected(self):
        child = self.root / "child"
        run(["git", "init", "-q", str(child)], check=True)
        (child / "child.cpp").write_text("original\n")
        run(["git", "-C", str(child), "add", "."], check=True)
        run(["git", "-C", str(child), "-c", "user.name=Test", "-c", "user.email=test@example.invalid",
             "commit", "-qm", "child"], check=True)
        run(["git", "-C", str(self.taglib), "-c", "protocol.file.allow=always", "submodule", "add",
             str(child), "child"], check=True)
        run(["git", "-C", str(self.taglib), "-c", "user.name=Test", "-c", "user.email=test@example.invalid",
             "commit", "-qam", "add submodule"], check=True)
        self.env["TAGLIB_COMMIT"] = run(["git", "-C", str(self.taglib), "rev-parse", "HEAD"], check=True).stdout.strip()
        (self.taglib / "child/injected.cpp").write_text("tampered\n")
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("pristine", result.stderr.lower())
        self.assertFalse(self.log.exists())

    def test_fresh_taglib_clone_is_independent_of_host_line_endings(self):
        run(["git", "-C", str(self.taglib), "tag", "vfixture"], check=True)
        origin = self.root / "taglib-origin"
        self.taglib.rename(origin)
        config = self.root / "gitconfig"
        config.write_text("[core]\n\tautocrlf = true\n")
        self.env.update(TAGLIB_URL=str(origin), GIT_CONFIG_GLOBAL=str(config))
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.log.read_text(), "verified zlib\nverified taglib\n")


class ImmutablePins(unittest.TestCase):
    def test_dependabot_checks_go_modules_weekly(self):
        result = run(["go", "run", "-mod=vendor", str(ROOT / "scripts/tests/parse_workflows.go"),
                      str(ROOT / ".github/dependabot.yml")], cwd=ROOT,
                     env=dict(os.environ, GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off"))
        self.assertEqual(result.returncode, 0, result.stderr)
        updates = json.loads(result.stdout)["dependabot.yml"]["updates"]
        self.assertIn({"package-ecosystem": "gomod", "directory": "/", "schedule": {"interval": "weekly"}}, updates)

    def test_actions_use_reviewed_node24_commits(self):
        expected = {
            "actions/checkout": "3d3c42e5aac5ba805825da76410c181273ba90b1",
            "actions/setup-go": "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
            "actions/upload-artifact": "cf430e030ddbb5b0abf93d22962f4752f3646cd9",
            "actions/download-artifact": "9000827ccba6bdab643e8b6fd33ac0654aef8333",
            "actions/attest-build-provenance": "4d101475d8b20a2381f78447822ac1eab6504dd8",
            "msys2/setup-msys2": "ec48f7c5447b3140e2b088413ae3a55687bccb6e",
        }
        for workflow in workflows().values():
            self.assertNotIn("FORCE_JAVASCRIPT_ACTIONS_TO_NODE24", workflow.get("env", {}))
            for job in workflow["jobs"].values():
                for step in job["steps"]:
                    if "uses" in step:
                        name, sha = step["uses"].split("@")
                        self.assertEqual(sha, expected[name], name)

    def test_all_action_references_are_full_commits(self):
        for path in (ROOT / ".github/workflows").glob("*.yml"):
            workflow = workflows()[path.name]
            actions = [step["uses"] for job in workflow["jobs"].values() for step in job["steps"] if "uses" in step]
            self.assertTrue(actions)
            for action in actions:
                self.assertRegex(action, r"@([0-9a-f]{40})$", str(path))

    def test_least_privilege_and_exact_checkouts(self):
        for path in (ROOT / ".github/workflows").glob("*.yml"):
            workflow = workflows()[path.name]
            self.assertEqual(workflow["permissions"], {"contents": "read"})
            for name, job in workflow["jobs"].items():
                permissions = job.get("permissions", workflow["permissions"])
                self.assertEqual(permissions.get("contents"), "write" if name == "publish-release" else "read")
                for permission in ("id-token", "attestations"):
                    self.assertEqual(permissions.get(permission), "write" if name == "attest-release" else None)
                for step in job["steps"]:
                    if step.get("uses", "").startswith("actions/checkout@"):
                        self.assertEqual(step["with"]["ref"], "${{ github.sha }}")
                        self.assertIs(step["with"]["persist-credentials"], False)
                    if "docker run" in step.get("run", ""):
                        self.fail("workflows must use the isolated Docker wrapper")

    def test_release_gate_and_tag_validation(self):
        jobs = workflows()["release.yml"]["jobs"]
        gate = jobs["release-gate"]
        validator = gate["steps"][0]["run"]
        self.assertIn("scripts/docker-build.sh test", gate["steps"][-1]["run"])
        for name, job in jobs.items():
            if name == "release-gate":
                continue
            needs = job["needs"]
            self.assertIn("release-gate", [needs] if isinstance(needs, str) else needs)
        cases = [("v1.2.3", "tag", True), ("v0.0.0-rc1", "tag", True),
                 ("v1.2.3-alpha.1+build.12", "tag", True), ("main", "branch", False),
                 ("v1.2.3", "branch", False), ("v01.2.3", "tag", False),
                 ("v1.2.3-01", "tag", False), ("v1.2", "tag", False),
                 ("v1.2.3\nmalicious", "tag", False), ("v1.2.3;id", "tag", False)]
        for tag, kind, valid in cases:
            with self.subTest(tag=tag, kind=kind):
                env = dict(os.environ, GITHUB_REF_NAME=tag, GITHUB_REF_TYPE=kind,
                           GITHUB_REF=("refs/tags/" if kind == "tag" else "refs/heads/") + tag,
                           GITHUB_SHA="a" * 40)
                result = run(["bash", "-e", "-c", validator], env=env)
                self.assertEqual(result.returncode == 0, valid, result.stderr)

    def test_container_pins_and_offline_toolchain(self):
        dockerfile = (ROOT / "Dockerfile.build").read_text()
        self.assertRegex(dockerfile, r"FROM golang:1\.27\.3-bookworm@sha256:[0-9a-f]{64}")
        self.assertRegex(dockerfile, r"# syntax=docker/dockerfile:1\.7@sha256:[0-9a-f]{64}")
        for value in ("GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "-mod=vendor"):
            self.assertIn(value, dockerfile)

    def test_windows_jobs_execute_filesystem_regressions_natively(self):
        for filename, job_name in (("ci.yml", "windows-amd64-build"), ("release.yml", "windows-release")):
            job = workflows()[filename]["jobs"][job_name]
            self.assertEqual(job["runs-on"], "windows-2022")
            tests = [step for step in job["steps"] if "go test " in step.get("run", "")]
            self.assertTrue(tests, filename + " must execute Windows tests, not only cross-compile")
            for package in ("./internal/securefile", "./config", "./internal/beatport", "./internal/validator"):
                self.assertTrue(any(package in step["run"] for step in tests), package)
            self.assertTrue(all(step["shell"] == "pwsh" for step in tests))
            msys_index = next(index for index, step in enumerate(job["steps"])
                              if step.get("uses", "").startswith("msys2/setup-msys2@"))
            for step in tests:
                self.assertLess(job["steps"].index(step), msys_index,
                                "native tests must not inherit MSYS2 temporary paths")
                self.assertEqual(step["env"]["GOTOOLCHAIN"], "local")
                self.assertEqual(step["env"]["GOPROXY"], "off")
                self.assertEqual(step["env"]["GOSUMDB"], "off")

    def test_native_arm64_vulnerability_scan_is_a_required_gate(self):
        for filename, name in (("ci.yml", "macos-arm64-build"), ("release.yml", "macos-arm64-release")):
            job = workflows()[filename]["jobs"][name]
            self.assertEqual(job["runs-on"], "macos-15")
            steps = job["steps"]
            scans = [(index, step) for index, step in enumerate(steps)
                     if step["name"] == "Check Go vulnerabilities"]
            self.assertEqual(len(scans), 1, filename + " must run govulncheck")
            index, step = scans[0]
            self.assertIn("build-release-deps.sh darwin-arm64", steps[index - 1]["run"])
            self.assertIn("build-release-target.sh darwin-arm64", steps[index + 1]["run"])
            self.assertEqual(step["env"]["GOTOOLCHAIN"], "local")
            self.assertEqual(str(step["env"]["CGO_ENABLED"]), "1")
            for key, suffix in (("CGO_CPPFLAGS", "/include"), ("CGO_LDFLAGS", "/lib")):
                self.assertIn("${{ runner.temp }}/deps/darwin-arm64" + suffix, step["env"][key])
            self.assertIn("go install golang.org/x/vuln/cmd/govulncheck@v1.9.0", step["run"])
            self.assertRegex(step["run"], r'govulncheck" ./\.\.\.(?:\n|$)')
            self.assertNotIn("continue-on-error", step)
            self.assertNotIn("if", step)
            self.assertNotIn("||", step["run"])
            setups = [entry for entry in steps[:index] if entry.get("uses", "").startswith("actions/setup-go@")]
            self.assertEqual(str(setups[-1]["with"]["go-version"]), "1.27.3")

    def test_release_sbom_contains_native_and_go_dependencies(self):
        jobs = workflows()["release.yml"]["jobs"]
        steps = jobs["attest-release"]["steps"]
        generators = [step for step in steps if step["name"] == "Generate dependency SBOMs"]
        self.assertEqual(len(generators), 1, "generate inventories once, before attestation")
        with tempfile.TemporaryDirectory() as directory:
            dist = Path(directory) / "dist"
            targets = ("container", "darwin-amd64", "darwin-arm64", "windows-amd64")
            for target in targets:
                record = dist / ("release-" + target) / ("build-tools-" + target + ".txt")
                record.parent.mkdir(parents=True)
                record.write_text(
                    "go version go1.27.3 darwin/arm64\n"
                    "\tdep\texample.com/dependency\tv1.2.3\th1:fixture\n"
                    "\tbuild\tCGO_ENABLED=1\n")
            result = run(["bash", "-e", "-c", generators[0]["run"]], cwd=directory,
                         env=dict(os.environ, GITHUB_REF_NAME="v1.2.3-rc1"))
            self.assertEqual(result.returncode, 0, result.stderr)
            files = list(dist.glob("release-sbom/sbom-*.cdx.json"))
            self.assertEqual(len(files), len(targets))
            self.assertEqual(len({path.name for path in files}), len(targets))
            for path in files:
                sbom = json.loads(path.read_text())
                self.assertEqual(sbom["bomFormat"], "CycloneDX")
                components = {(item["name"], item["version"]) for item in sbom["components"]}
                self.assertIn(("example.com/dependency", "v1.2.3"), components)
                self.assertIn(("TagLib", "2.3.2"), components)
                self.assertIn(("zlib", "1.3.2"), components)


if __name__ == "__main__":
    unittest.main()
