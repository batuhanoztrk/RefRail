"""Validate generated multi-file manifests against pinned Microsoft schemas."""

import json
import pathlib
import sys

import jsonschema
import yaml


def main(root):
    schema_root = pathlib.Path(__file__).with_name("schemas")
    files = sorted(pathlib.Path(root).rglob("*.yaml"))
    if len(files) != 3:
        raise ValueError(f"Expected exactly three WinGet manifests, found {len(files)}")
    identity = None
    types = set()
    for path in files:
        manifest = yaml.safe_load(path.read_text(encoding="utf-8"))
        kind = manifest["ManifestType"]
        if kind not in {"installer", "defaultLocale", "version"} or kind in types:
            raise ValueError(f"Unexpected or duplicate manifest type: {kind}")
        types.add(kind)
        schema = json.loads((schema_root / f"{kind}.json").read_text())
        jsonschema.Draft7Validator(schema).validate(manifest)
        current = manifest["PackageIdentifier"], manifest["PackageVersion"]
        if identity is not None and current != identity:
            raise ValueError("Package identifier/version must match in all manifests")
        identity = current
        expected = pathlib.Path("manifests", current[0][0].lower(), *current[0].split("."), current[1])
        if path.parent.relative_to(root) != expected:
            raise ValueError(f"Unexpected WinGet directory layout: {path}")
        print(f"Valid WinGet {kind}: {path}")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("Usage: validate-winget.py <generated-winget-root>")
    main(pathlib.Path(sys.argv[1]))
