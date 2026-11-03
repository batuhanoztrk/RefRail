"""Publish a verified release through Portainer, then check the private source."""

import argparse
import hashlib
import json
import os
import pathlib
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import zipfile
from dataclasses import dataclass, field

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[2]
GIT_VERSION = "2.56.0.4"
GIT_ROOT = ROOT / "deploy/rewinged/git"
STACK_MARKER = "# refrail-private-winget-stack-v1"
MANAGED_ENV = ("REFRAIL_TAG", "REFRAIL_MANIFEST_SHA256", "REFRAIL_DEPLOY_REVISION")


def https_url(value):
    url = urllib.parse.urlsplit(value)
    if (url.scheme != "https" or not url.hostname or url.username or url.password
            or url.query or url.fragment):
        raise ValueError("Use an HTTPS URL without credentials, query or fragment")
    return value.rstrip("/")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class HTTP:
    def __init__(self):
        # Never forward the Portainer key on a redirect, including same-host redirects.
        self.opener = urllib.request.build_opener(NoRedirect())

    def open(self, url, headers=None, payload=None, timeout=60):
        request_headers = {"User-Agent": "RefRail-Distribution/1.0", **(headers or {})}
        body = None
        if payload is not None:
            body = json.dumps(payload).encode()
            request_headers["Content-Type"] = "application/json"
        req = urllib.request.Request(url, headers=request_headers, data=body,
                                     method="PUT" if body is not None else "GET")
        try:
            return self.opener.open(req, timeout=timeout)
        except urllib.error.HTTPError as exc:
            # Response bodies may contain stack environment values or credentials.
            exc.close()
            raise RuntimeError(f"HTTP request failed (status {exc.code})") from None
        except (urllib.error.URLError, TimeoutError, OSError):
            raise RuntimeError("HTTP connection failed; check HTTPS and reachability") from None

    def json(self, url, headers=None, payload=None, timeout=60):
        with self.open(url, headers, payload, timeout) as response:
            data = response.read(4 * 1024 * 1024 + 1)
        if len(data) > 4 * 1024 * 1024:
            raise RuntimeError("API response exceeds size limit")
        try:
            return json.loads(data)
        except (ValueError, UnicodeError):
            raise RuntimeError("API returned invalid JSON") from None

    def digest(self, url):
        digest = hashlib.sha256()
        size = 0
        with self.open(url) as response:
            while True:
                chunk = response.read(1024 * 1024)
                if not chunk:
                    break
                size += len(chunk)
                if size > 256 * 1024 * 1024:
                    raise RuntimeError("Installer exceeds download size limit")
                digest.update(chunk)
        return digest.hexdigest()


@dataclass
class Config:
    portainer: str
    stack_id: int
    key: str = field(repr=False)
    stack_name: str = "winget"
    source: str = "https://winget.noktabilisim.com/api"

    @classmethod
    def from_env(cls):
        required = ("PRIVATE_WINGET_PORTAINER_URL", "PRIVATE_WINGET_STACK_ID",
                    "PRIVATE_WINGET_PORTAINER_API_KEY")
        for name in required:
            if not os.environ.get(name):
                raise ValueError(f"Missing {name}")
        stack_id = os.environ[required[1]]
        if not re.fullmatch(r"[1-9][0-9]*", stack_id):
            raise ValueError("PRIVATE_WINGET_STACK_ID must be a positive integer")
        return cls(https_url(os.environ[required[0]]), int(stack_id), os.environ[required[2]],
                   os.getenv("PRIVATE_WINGET_STACK_NAME", "winget"),
                   https_url(os.getenv("PRIVATE_WINGET_SOURCE_URL",
                                       "https://winget.noktabilisim.com/api")))


def inspect_stack(config, http):
    url = f"{config.portainer}/api/stacks/{config.stack_id}"
    headers = {"X-API-Key": config.key}
    stack = http.json(url, headers)
    if (stack.get("Id") != config.stack_id or stack.get("Name") != config.stack_name
            or stack.get("Type") != 2 or not isinstance(stack.get("EndpointId"), int)
            or stack["EndpointId"] <= 0 or stack.get("GitConfig")):
        raise ValueError("Expected the configured file-based Docker Standalone stack")
    content = http.json(url + "/file", headers).get("StackFileContent", "")
    if not content.startswith(STACK_MARKER + "\n"):
        raise ValueError("Update the Portainer stack from deploy/rewinged/stack-arm64.yml first")
    try:
        template = yaml.safe_load(content)
    except yaml.YAMLError:
        raise ValueError("Portainer stack contains invalid YAML") from None
    for service in ("manifests-init", "rewinged"):
        env = template.get("services", {}).get(service, {}).get("environment", {})
        if any(not isinstance(env.get(name), str)
               or not re.fullmatch(r"\$\{" + name + r"(?::-[^{}]*)?\}", env[name])
               for name in MANAGED_ENV):
            raise ValueError("Stack must interpolate all three managed variables in init and server")
    env = stack.get("Env") or []
    if (not isinstance(env, list) or any(not isinstance(pair, dict)
            or not isinstance(pair.get("name"), str) or not isinstance(pair.get("value"), str)
            for pair in env) or len({pair["name"] for pair in env}) != len(env)):
        raise ValueError("Invalid or duplicate Portainer stack environment entries")
    return stack, content


def archive_digest(archive, metadata):
    """Approve only the exact three manifests regenerated from verified runtime ZIPs."""
    expected = {p.relative_to(metadata).as_posix(): p.read_bytes()
                for p in metadata.rglob("*.yaml")}
    if len(expected) != 3 or archive.stat().st_size > 512 * 1024:
        raise ValueError("Expected a small archive and exactly three generated manifests")
    actual = {}
    with zipfile.ZipFile(archive) as package:
        for info in package.infolist():
            path = pathlib.PurePosixPath(info.filename)
            if path.is_absolute() or ".." in path.parts or "\\" in info.filename:
                raise ValueError("Unsafe path in manifest archive")
            if info.is_dir():
                continue
            if info.filename in actual or info.filename not in expected or info.file_size > 64 * 1024:
                raise ValueError("Unexpected or duplicate file in manifest archive")
            actual[info.filename] = package.read(info)
    if actual != expected:
        raise ValueError("Published manifest archive differs from regenerated release metadata")
    return hashlib.sha256(archive.read_bytes()).hexdigest()


def expected_packages(metadata, tag):
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError("Expected a canonical stable release tag")
    packages = {}
    for root, version in ((metadata, tag[1:]), (GIT_ROOT, GIT_VERSION)):
        files = list(root.rglob("*.installer.yaml"))
        if len(files) != 1:
            raise ValueError("Expected exactly one installer manifest per package")
        manifest = yaml.safe_load(files[0].read_text())
        if str(manifest["PackageVersion"]) != version:
            raise ValueError("Installer version differs from requested release")
        installers = []
        for entry in manifest["Installers"]:
            effective = {**manifest, **entry}
            installers.append({key: effective[key] for key in (
                "Architecture", "Scope", "InstallerSha256", "InstallerType",
                "NestedInstallerType", "NestedInstallerFiles", "Dependencies") if key in effective})
        packages[manifest["PackageIdentifier"]] = (version, installers)
    for _, installers in packages.values():
        for installer in installers:
            for dep in installer.get("Dependencies", {}).get("PackageDependencies", []):
                if dep["PackageIdentifier"] not in packages:
                    raise ValueError("Private source does not include all package dependencies")
    return packages


def source_installers(config, http, packages):
    downloads = {}
    origin = urllib.parse.urlsplit(config.source)
    for identifier, (version, expected) in packages.items():
        data = http.json(config.source + "/packageManifests/" + urllib.parse.quote(identifier),
                         {"Version": "1.10.0"})["Data"]
        versions = [v for v in data.get("Versions", []) if v["PackageVersion"] == version]
        if data.get("PackageIdentifier") != identifier or len(versions) != 1:
            raise RuntimeError("Private source has not ingested both requested package versions")
        actual = versions[0].get("Installers", [])
        if len(actual) != len(expected):
            raise RuntimeError("Private source installer count differs from release metadata")
        for entry in expected:
            matches = [i for i in actual if i.get("Architecture") == entry["Architecture"]
                       and i.get("Scope", "") == entry.get("Scope", "")]
            if len(matches) != 1:
                raise RuntimeError("Private source is missing an expected architecture/scope")
            installer = matches[0]
            for key, value in entry.items():
                found = installer.get(key)
                if key == "InstallerSha256":
                    value, found = value.lower(), str(found).lower()
                elif key == "Dependencies":
                    # rewinged emits an empty MinimumVersion when it was unspecified.
                    value = [(d["PackageIdentifier"], d.get("MinimumVersion", ""))
                             for d in value.get("PackageDependencies", [])]
                    found = [(d["PackageIdentifier"], d.get("MinimumVersion", ""))
                             for d in (found or {}).get("PackageDependencies", [])]
                if found != value:
                    raise RuntimeError("Private source installer metadata differs from approved manifests")
            url = https_url(installer["InstallerUrl"])
            parsed = urllib.parse.urlsplit(url)
            if (parsed.netloc != origin.netloc or not parsed.path.startswith("/installers/")
                    or parsed.path.rsplit("/", 1)[-1].lower() != entry["InstallerSha256"].lower()):
                raise RuntimeError("Expected an internalized installer on the private source origin")
            downloads[url] = entry["InstallerSha256"].lower()
    return downloads


def wait_for_source(config, http, packages, timeout=600):
    deadline = time.monotonic() + timeout
    verified = set()
    while True:
        try:
            for url, expected in source_installers(config, http, packages).items():
                if (url, expected) not in verified:
                    if http.digest(url) != expected:
                        raise RuntimeError("Cached installer SHA256 mismatch; inspect rewinged's installer cache")
                    verified.add((url, expected))
            return len(verified)
        except (RuntimeError, KeyError, TypeError, ValueError) as exc:
            if time.monotonic() >= deadline:
                raise RuntimeError("Private source verification timed out; inspect init/server logs "
                                   "and cached installers. Last check: " + str(exc)) from None
            time.sleep(10)


def publish(config, http, tag, archive, metadata):
    digest = archive_digest(archive, metadata)
    packages = expected_packages(metadata, tag)
    stack, content = inspect_stack(config, http)
    updated = {"REFRAIL_TAG": tag, "REFRAIL_MANIFEST_SHA256": digest,
               "REFRAIL_DEPLOY_REVISION": uuid.uuid4().hex}
    env = [pair for pair in stack.get("Env", []) if pair["name"] not in updated]
    env.extend({"name": name, "value": value} for name, value in updated.items())
    http.json(f"{config.portainer}/api/stacks/{config.stack_id}?endpointId={stack['EndpointId']}",
              {"X-API-Key": config.key},
              {"StackFileContent": content, "Env": env, "PullImage": False}, timeout=600)
    print("Portainer accepted the update; checking RefRail, Git.Git and their installers.", flush=True)
    count = wait_for_source(config, http, packages)
    print(f"Private WinGet verified: {tag}, Git.Git {GIT_VERSION}, {count} unique installers.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("preflight", "verify-archive", "publish"))
    parser.add_argument("--tag")
    parser.add_argument("--archive", type=pathlib.Path)
    parser.add_argument("--metadata", type=pathlib.Path)
    args = parser.parse_args()
    if args.command == "preflight":
        inspect_stack(Config.from_env(), HTTP())
        print("Private WinGet Portainer configuration is ready.")
    else:
        if args.archive is None or args.metadata is None:
            parser.error("--archive and --metadata are required")
        if args.command == "verify-archive":
            print(archive_digest(args.archive, args.metadata))
        else:
            if args.tag is None:
                parser.error("--tag is required")
            publish(Config.from_env(), HTTP(), args.tag, args.archive, args.metadata)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        # Do not dump tracebacks or server responses from a privileged deployment client.
        print(f"Private WinGet error: {error}", file=sys.stderr)
        sys.exit(1)
