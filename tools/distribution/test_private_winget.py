import copy
import datetime
import hashlib
import http.server
import json
import pathlib
import tempfile
import threading
import unittest
import warnings
import zipfile
from unittest import mock

import yaml

import private_winget as publisher


STACK = (publisher.ROOT / "deploy/rewinged/stack-arm64.yml").read_text()


class FakeHTTP:
    def __init__(self):
        self.stack = {"Id": 42, "Name": "winget", "Type": 2, "EndpointId": 7,
                      "Env": [{"name": "UNRELATED", "value": "keep-me"},
                              {"name": "REFRAIL_TAG", "value": "v0.0.1"}]}
        self.content = STACK
        self.responses = {}
        self.digests = {}
        self.requests = []

    def json(self, url, headers=None, payload=None, timeout=60):
        self.requests.append((url, headers, payload, timeout))
        if payload is not None:
            return self.stack
        if "/api/stacks/42" in url:
            return {"StackFileContent": self.content} if url.endswith("/file") else self.stack
        if url not in self.responses:
            raise RuntimeError("HTTP request failed (status 404)")
        return self.responses[url]

    def digest(self, url):
        return self.digests[url]


class PublisherTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        self.metadata = self.root / "winget"
        path = self.metadata / "manifests/b/batuhanoztrk/RefRail/0.1.0"
        path.mkdir(parents=True)
        self.installer = {
            "PackageIdentifier": "batuhanoztrk.RefRail", "PackageVersion": "0.1.0",
            "InstallerType": "zip", "NestedInstallerType": "portable",
            "NestedInstallerFiles": [{"RelativeFilePath": "refrail\\bin\\refrail.exe",
                                      "PortableCommandAlias": "refrail"}],
            "Dependencies": {"PackageDependencies": [{"PackageIdentifier": "Git.Git"}]},
            "Installers": [{"Architecture": "x64", "InstallerSha256": "a" * 64},
                           {"Architecture": "arm64", "InstallerSha256": "b" * 64}],
        }
        (path / "batuhanoztrk.RefRail.installer.yaml").write_text(yaml.safe_dump(self.installer))
        (path / "batuhanoztrk.RefRail.yaml").write_text("version fixture\n")
        (path / "batuhanoztrk.RefRail.locale.en-US.yaml").write_text("locale fixture\n")
        self.archive = self.root / "winget-manifests.zip"
        with zipfile.ZipFile(self.archive, "w") as archive:
            for file in self.metadata.rglob("*.yaml"):
                archive.write(file, file.relative_to(self.metadata))
        self.config = publisher.Config("https://portainer.example", 42, "secret-key")
        self.http = FakeHTTP()

    def populate_source(self):
        packages = publisher.expected_packages(self.metadata, "v0.1.0")
        for identifier, (version, expected) in packages.items():
            installers = copy.deepcopy(expected)
            for installer in installers:
                digest = installer["InstallerSha256"].lower()
                installer["InstallerUrl"] = "https://winget.noktabilisim.com/installers/" + digest
                self.http.digests[installer["InstallerUrl"]] = digest
                for dep in installer.get("Dependencies", {}).get("PackageDependencies", []):
                    dep["MinimumVersion"] = ""
            self.http.responses[self.config.source + "/packageManifests/" + identifier] = {
                "Data": {"PackageIdentifier": identifier,
                         "Versions": [{"PackageVersion": version, "Installers": installers}]}}
        return packages

    def test_archive_must_match_generated_manifests_exactly(self):
        self.assertEqual(publisher.archive_digest(self.archive, self.metadata),
                         hashlib.sha256(self.archive.read_bytes()).hexdigest())
        next(self.metadata.rglob("*.yaml")).write_text("changed")
        with self.assertRaisesRegex(ValueError, "differs"):
            publisher.archive_digest(self.archive, self.metadata)

    def test_archive_rejects_extra_duplicate_and_unsafe_paths(self):
        cases = ["../escape.yaml", "/absolute.yaml", "other.txt",
                 next(self.metadata.rglob("*.yaml")).relative_to(self.metadata).as_posix()]
        original = self.archive.read_bytes()
        for name in cases:
            with self.subTest(name=name):
                self.archive.write_bytes(original)
                with zipfile.ZipFile(self.archive, "a") as archive:
                    with warnings.catch_warnings():
                        warnings.simplefilter("ignore", UserWarning)
                        archive.writestr(name, "bad")
                with self.assertRaises(ValueError):
                    publisher.archive_digest(self.archive, self.metadata)

    def test_preflight_rejects_wrong_stack_and_unprepared_template(self):
        for key, value in (("Id", 99), ("Name", "production"), ("Type", 1),
                           ("EndpointId", 0), ("GitConfig", {"URL": "https://example.com"})):
            with self.subTest(key=key):
                original = copy.deepcopy(self.http.stack)
                self.http.stack[key] = value
                with self.assertRaises(ValueError):
                    publisher.inspect_stack(self.config, self.http)
                self.http.stack = original
        for content in (STACK.replace(publisher.STACK_MARKER, "# old stack"),
                        STACK.replace("${REFRAIL_TAG:-v0.1.0}", "v0.1.0"),
                        STACK.replace("${REFRAIL_TAG:-v0.1.0}", "${REFRAIL_TAG_WRONG}")):
            self.http.content = content
            with self.assertRaises(ValueError):
                publisher.inspect_stack(self.config, self.http)
        self.assertFalse(any(request[2] for request in self.http.requests))

    def test_preflight_errors_do_not_disclose_stack_yaml(self):
        self.http.content = publisher.STACK_MARKER + "\nsecret: [private-secret\ninvalid:"
        with self.assertRaises(ValueError) as result:
            publisher.inspect_stack(self.config, self.http)
        self.assertNotIn("private-secret", str(result.exception))
        self.assertNotIn("secret-key", repr(self.config))

    def test_preflight_rejects_duplicate_stack_environment(self):
        self.http.stack["Env"] *= 2
        with self.assertRaisesRegex(ValueError, "duplicate"):
            publisher.inspect_stack(self.config, self.http)

    def test_publish_preserves_stack_and_unrelated_environment_and_forces_retry(self):
        self.populate_source()
        with mock.patch.object(publisher, "wait_for_source", return_value=4):
            publisher.publish(self.config, self.http, "v0.1.0", self.archive, self.metadata)
            publisher.publish(self.config, self.http, "v0.1.0", self.archive, self.metadata)
        updates = [request for request in self.http.requests if request[2] is not None]
        self.assertEqual(len(updates), 2)
        for url, headers, payload, timeout in updates:
            self.assertEqual(url, "https://portainer.example/api/stacks/42?endpointId=7")
            self.assertEqual(headers, {"X-API-Key": "secret-key"})
            self.assertEqual(payload["StackFileContent"], STACK)
            self.assertFalse(payload["PullImage"])
            env = {pair["name"]: pair["value"] for pair in payload["Env"]}
            self.assertEqual(env["UNRELATED"], "keep-me")
            self.assertEqual(env["REFRAIL_TAG"], "v0.1.0")
            self.assertEqual(env["REFRAIL_MANIFEST_SHA256"],
                             hashlib.sha256(self.archive.read_bytes()).hexdigest())
            self.assertEqual(len(env), len(payload["Env"]))
            self.assertEqual(timeout, 600)
        self.assertNotEqual(updates[0][2]["Env"][-1], updates[1][2]["Env"][-1])

    def test_invalid_archive_does_not_update_portainer(self):
        next(self.metadata.rglob("*.yaml")).write_text("changed")
        with self.assertRaises(ValueError):
            publisher.publish(self.config, self.http, "v0.1.0", self.archive, self.metadata)
        self.assertFalse(self.http.requests)

    def test_source_checks_refrail_and_git_all_architectures_and_scopes(self):
        packages = self.populate_source()
        self.assertEqual(publisher.wait_for_source(self.config, self.http, packages, timeout=0), 4)
        self.assertTrue(all(headers == {"Version": "1.10.0"}
                            for _, headers, _, _ in self.http.requests))

    def test_source_without_git_fails_even_when_refrail_exists(self):
        packages = self.populate_source()
        del self.http.responses[self.config.source + "/packageManifests/Git.Git"]
        with self.assertRaisesRegex(RuntimeError, "timed out"):
            publisher.wait_for_source(self.config, self.http, packages, timeout=0)

    def test_source_rejects_wrong_version_hash_metadata_or_external_installer(self):
        for mutate in (lambda v: v.update(PackageVersion="0.0.1"),
                       lambda v: v["Installers"][0].update(InstallerSha256="c" * 64),
                       lambda v: v["Installers"][0].update(NestedInstallerFiles=[]),
                       lambda v: v["Installers"][0].update(Dependencies={}),
                       lambda v: v["Installers"][0].update(InstallerUrl="https://evil.example/installers/abc")):
            with self.subTest(mutate=mutate):
                packages = self.populate_source()
                version = self.http.responses[self.config.source + "/packageManifests/batuhanoztrk.RefRail"]["Data"]["Versions"][0]
                mutate(version)
                with self.assertRaises(RuntimeError):
                    publisher.wait_for_source(self.config, self.http, packages, timeout=0)

    def test_source_rejects_partial_cache_file(self):
        packages = self.populate_source()
        url = next(iter(self.http.digests))
        self.http.digests[url] = hashlib.sha256(b"").hexdigest()
        with self.assertRaisesRegex(RuntimeError, "SHA256 mismatch"):
            publisher.wait_for_source(self.config, self.http, packages, timeout=0)

    def test_source_retries_ingestion(self):
        packages = self.populate_source()
        real = publisher.source_installers
        with mock.patch.object(publisher, "source_installers", side_effect=[
                RuntimeError("ingesting"), real(self.config, self.http, packages)]), \
                mock.patch.object(publisher.time, "sleep") as sleep:
            self.assertEqual(publisher.wait_for_source(self.config, self.http, packages), 4)
            sleep.assert_called_once_with(10)

    def test_tag_and_dependency_closure_are_validated(self):
        for tag in ("v01.0.0", "v0.1.0-beta", "0.1.0", "v0.2.0"):
            with self.assertRaises(ValueError):
                publisher.expected_packages(self.metadata, tag)
        installer_path = next(self.metadata.rglob("*.installer.yaml"))
        self.installer["Dependencies"]["PackageDependencies"][0]["PackageIdentifier"] = "Unknown.Package"
        installer_path.write_text(yaml.safe_dump(self.installer))
        with self.assertRaisesRegex(ValueError, "dependencies"):
            publisher.expected_packages(self.metadata, "v0.1.0")

    def test_configuration_requires_https_and_selected_credentials(self):
        env = {"PRIVATE_WINGET_PORTAINER_URL": "https://portainer.example/",
               "PRIVATE_WINGET_STACK_ID": "42", "PRIVATE_WINGET_PORTAINER_API_KEY": "secret-key"}
        with mock.patch.dict(publisher.os.environ, env, clear=True):
            self.assertEqual(publisher.Config.from_env().portainer, "https://portainer.example")
        for changes in ({"PRIVATE_WINGET_STACK_ID": "0"}, {"PRIVATE_WINGET_STACK_ID": "1&x=2"},
                        {"PRIVATE_WINGET_PORTAINER_API_KEY": ""},
                        {"PRIVATE_WINGET_PORTAINER_URL": "http://portainer.example"},
                        {"PRIVATE_WINGET_PORTAINER_URL": "https://user:password@example.com"},
                        {"PRIVATE_WINGET_SOURCE_URL": "https://winget.example/api?key=secret"}):
            with self.subTest(changes=changes), mock.patch.dict(publisher.os.environ, {**env, **changes}, clear=True):
                with self.assertRaises(ValueError):
                    publisher.Config.from_env()

    def test_dependency_pins_match_the_stack_and_cover_both_architectures(self):
        manifest_dir = publisher.GIT_ROOT / "manifests/g/Git/Git" / publisher.GIT_VERSION
        for path in manifest_dir.glob("*.yaml"):
            self.assertIn(hashlib.sha256(path.read_bytes()).hexdigest() + "  " + path.name, STACK)
        manifest = yaml.safe_load((manifest_dir / "Git.Git.installer.yaml").read_text())
        self.assertIn("/" + publisher.GIT_VERSION, STACK)
        self.assertEqual({i["Architecture"] for i in manifest["Installers"]}, {"x64", "arm64"})
        self.assertFalse(manifest.get("Dependencies"))
        for installer in manifest["Installers"]:
            self.assertFalse(installer.get("Dependencies"))
        # rewinged maps schema 1.12 to 1.10 (except font); these pins use no new fields.
        import jsonschema
        for path in manifest_dir.glob("*.yaml"):
            document = yaml.safe_load(path.read_text())
            schema = json.loads((publisher.ROOT / "tools/packaging/schemas" /
                                 (document["ManifestType"] + ".json")).read_text())
            document["ManifestVersion"] = "1.10.0"
            if isinstance(document.get("ReleaseDate"), datetime.date):
                document["ReleaseDate"] = document["ReleaseDate"].isoformat()
            jsonschema.Draft7Validator(schema).validate(document)


class HTTPTests(unittest.TestCase):
    def test_redirect_never_forwards_key_and_errors_never_include_response_body(self):
        paths = []

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                paths.append(self.path)
                self.send_response(302 if self.path == "/redirect" else 403)
                self.send_header("Location", "/stolen")
                self.end_headers()
                self.wfile.write(b"private-response-secret")

            def log_message(self, *args):
                pass

        server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        try:
            for path in ("/redirect", "/forbidden"):
                with self.assertRaises(RuntimeError) as result:
                    publisher.HTTP().json(f"http://127.0.0.1:{server.server_port}{path}",
                                          {"X-API-Key": "secret-key"})
                self.assertNotIn("private-response-secret", str(result.exception))
                self.assertNotIn("secret-key", str(result.exception))
            self.assertEqual(paths, ["/redirect", "/forbidden"])
        finally:
            server.shutdown()
            server.server_close()
            worker.join()


if __name__ == "__main__":
    unittest.main()
