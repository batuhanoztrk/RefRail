# Publishing RefRail

The source repository is `batuhanoztrk/RefRail`. Homebrew uses
`batuhanoztrk/homebrew-tap` and WinGet uses `batuhanoztrk.RefRail`, with publisher
`batuhanoztrk`. These files prepare publication; adding them locally does not
create repositories, push tags, publish releases, or submit packages.

## First-time GitHub setup

1. Push the source and workflows to the public
   [RefRail repository](https://github.com/batuhanoztrk/RefRail). This checkout
   initially has no remote; add `origin` if needed:

   ```sh
   git remote add origin https://github.com/batuhanoztrk/RefRail.git
   git push -u origin main
   ```

2. Create a public, initialized `batuhanoztrk/homebrew-tap` repository. A README
   commit is required so Actions can check out and update its default branch:

   ```sh
   gh repo create batuhanoztrk/homebrew-tap --public --add-readme \
     --description "Homebrew formulae maintained by batuhanoztrk"
   ```

   The `homebrew-` repository prefix is conventional. Homebrew exposes this repo
   as `batuhanoztrk/tap`, so users install with
   `brew install batuhanoztrk/tap/refrail`.

3. Under **RefRail → Settings → Secrets and variables → Actions**, configure:

   | Kind | Name | Value / access |
   | --- | --- | --- |
   | Variable (optional) | `HOMEBREW_TAP_REPOSITORY` | Defaults to `batuhanoztrk/homebrew-tap` |
   | Variable (optional) | `WINGET_PACKAGE_IDENTIFIER` | Defaults to `batuhanoztrk.RefRail`; keep stable after acceptance |
   | Variable (optional) | `WINGET_PUBLISHER` | Defaults to `batuhanoztrk` |
   | Secret | `HOMEBREW_TAP_TOKEN` | Fine-grained PAT restricted to the tap, **Contents: read/write** |
   | Secret | `WINGET_CREATE_GITHUB_TOKEN` | WinGetCreate-compatible PAT, with access to create/update your public `winget-pkgs` fork and submit upstream PRs |

   The normal `GITHUB_TOKEN` cannot write to a different tap repository. A
   WinGet token must also work with the upstream repository and your fork;
   follow [Microsoft's token instructions](https://github.com/microsoft/winget-create/blob/v1.12.13.0/README.md#github-personal-access-token-classic-permissions).
   Secrets are needed only for selected distribution channels. Draft-release
   creation uses the job-scoped `GITHUB_TOKEN` with `contents: write`.
   The tap workflow pushes directly to its default branch; allow this token to
   update that branch, or copy the generated formula through your normal PR
   process instead if branch protection requires reviews.

## Release sequence

1. Commit reviewed changes and let **CI** pass. CI builds native bundles and runs
   tests in English and Turkish plus the runtime-free sandbox on macOS, Linux,
   and Windows. A separate job builds all six archives, validates WinGet YAML
   against the vendored Microsoft schemas, and tests the Homebrew formula on
   macOS using the staged ZIP, before any public release exists.

2. Push a new stable SemVer tag. Use `v0.1.0` for the first release only if it
   does not already exist:

   ```sh
   git tag -a v0.1.0 -m "RefRail v0.1.0"
   git push origin v0.1.0
   ```

   **Release** checks the tag, tests that exact source, and creates a **draft**
   GitHub Release. You can also dispatch Release with an existing stable tag.
   Supported tags have the form `vMAJOR.MINOR.PATCH`; prereleases are rejected
   by this initial stable-channel workflow.

3. Review the draft's notes and these nine assets, then publish it in GitHub:
   six `refrail_vX.Y.Z_<os>_<arch>.zip` files, `SHA256SUMS`,
   `homebrew-tap.zip`, and `winget-manifests.zip`. `SHA256SUMS` covers the six
   runtime ZIPs. Each bundle also has its own file manifest and licenses.
   Release retries may finish an existing draft; published releases are never
   overwritten by the workflow.

4. In **Actions → Distribute → Run workflow**, select the published tag and
   desired channels. Homebrew defaults on; WinGet submission is an explicit
   checkbox. The workflow refuses drafts and prereleases, checks selected
   credentials, downloads and verifies the published runtime assets, and
   regenerates metadata. It then updates the tap and/or submits a PR to
   `microsoft/winget-pkgs`. WinGet availability requires Microsoft's validation
   and acceptance; creating a PR does not immediately publish a package. Review
   and close an existing pending PR before retrying a WinGet submission, to
   avoid duplicates.

Do not reuse a released tag for changed content. Publish a new version instead.
Keep the package identifier and publisher spelling stable across updates.
Homebrew's tap updates are immediately available through `brew update`; WinGet
updates use the same PR and acceptance process as the first submission.

## Generate metadata locally

Run from the source root with Go 1.27+, Git, and network access for uncached
Lefthook downloads:

```sh
go run ./tools/release --version v0.1.0
go run ./tools/packaging --version v0.1.0 \
  --dist dist/v0.1.0 --out dist/packages-v0.1.0
ruby -c dist/packages-v0.1.0/homebrew/Formula/refrail.rb
python3 -m venv .cache/packaging-venv
.cache/packaging-venv/bin/pip install -r tools/packaging/requirements.txt
.cache/packaging-venv/bin/python tools/packaging/validate-winget.py dist/packages-v0.1.0/winget
```

Use a fresh `--out` path; the generator refuses to overwrite existing output.
It requires all six ZIPs, matching SHA256SUMS, and matching version/platform
manifests, including all required files and their individual digests. ZIP paths,
links, duplicate entries, and expanded size are checked before extraction.
`--repository`, `--winget-id`, and `--publisher` can override the
documented identity defaults. Python 3.9+ and the two Python dependencies are
maintainer-only, with no change to RefRail's offline runtime requirements.

Homebrew output is `homebrew/Formula/refrail.rb`. WinGet output is
`winget/manifests/b/batuhanoztrk/RefRail/<version>/` with version, installer, and
English default-locale YAML files. Runtime ZIPs contain the complete bundle
under `refrail/`, keeping WinGet's portable index outside the verified root.
The ZIP installer exposes `refrail/bin/refrail.exe`
as `refrail`, keeps the accompanying bundle intact, and declares `Git.Git`.
Lefthook stays private; RefRail continues to use its bundled copy.

On a Windows test machine, enable local manifests if necessary, then run:

```powershell
winget validate --manifest dist/packages-v0.1.0/winget/manifests/b/batuhanoztrk/RefRail/0.1.0
winget install --manifest dist/packages-v0.1.0/winget/manifests/b/batuhanoztrk/RefRail/0.1.0
refrail version --json
refrail install --repo C:\Projects\test-clone
refrail doctor --repo C:\Projects\test-clone
```

This installation downloads the published release URLs: publish the release
before the Windows package-manager pilot. Schema validation and cross-building
do not prove a successful WinGet installation. CI executes one native
architecture per OS; the other target binaries are cross-built. SmartScreen,
antivirus scanning, actual WinGet installation, and upstream review remain
external release checks. The binaries are not code-signed by these workflows.

## Upgrade and uninstall

Package managers update the CLI and its distribution bundle. Existing clones
keep their verified copies until `refrail update --repo <path>` is run. This
preserves per-clone versions and baselines. Removing the package-manager CLI
does not remove active hooks; run `refrail uninstall --repo <path>` first for
each clone when you want to remove RefRail's hooks. Cached clone bundles are
retained intentionally.

## References

- [Homebrew taps](https://docs.brew.sh/How-to-Create-and-Maintain-a-Tap)
- [Homebrew formula cookbook](https://docs.brew.sh/Formula-Cookbook)
- [WinGet manifest documentation](https://learn.microsoft.com/en-us/windows/package-manager/package/manifest)
- [WinGetCreate submit command](https://github.com/microsoft/winget-create/blob/v1.12.13.0/doc/submit.md)

GitHub Actions are pinned to commit hashes. WinGetCreate is pinned to version
1.12.13.0 and SHA-256. Vendored WinGet schemas are version 1.10.0; provenance
and the upstream license live under `tools/packaging/schemas`. Review and update
these pins explicitly when upgrading the release toolchain.
