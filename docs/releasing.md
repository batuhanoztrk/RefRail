# Publishing RefRail

The source repository is `batuhanoztrk/RefRail`. Homebrew uses
`batuhanoztrk/homebrew-tap` and WinGet uses `batuhanoztrk.RefRail`, with publisher
`batuhanoztrk`. The optional private WinGet source is
`https://winget.noktabilisim.com/api`, managed through Portainer. These files prepare publication; adding them locally does not
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
   follow [Microsoft's token instructions](https://github.com/microsoft/winget-create/blob/v1.12.13.0/doc/token.md).
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
   desired channels: **homebrew**, **winget** (Microsoft community PR), and
   **private_winget** (Portainer). Homebrew defaults on; both WinGet channels
   default off. Any combination is supported; turn Homebrew off for a private-only
   run. The workflow refuses drafts and prereleases, checks selected
   credentials, downloads and verifies the published runtime assets, and
   regenerates metadata. It then updates the tap and/or submits a PR to
   `microsoft/winget-pkgs`, and/or updates the private stack. Private distribution
   also verifies the published manifest archive against the regenerated metadata,
   checks Portainer access and stack compatibility before any channel changes,
   and verifies RefRail, Git.Git and all four unique Windows installers after the
   update. Selected channels run independently after preparation; a later failure
   does not roll back another channel. Microsoft WinGet availability requires Microsoft's validation
   and acceptance; creating a PR does not immediately publish a package. Review
   and close an existing pending PR before retrying a WinGet submission, to
   avoid duplicates.

Do not reuse a released tag for changed content. Publish a new version instead.
Keep the package identifier and publisher spelling stable across updates.
Homebrew's tap updates are immediately available through `brew update`; Microsoft WinGet
updates use the same PR and acceptance process as the first submission.

## Private WinGet: one-time Portainer setup

The checked-in [ARM64 stack](../deploy/rewinged/stack-arm64.yml) targets
**Docker Standalone**, matching the existing Portainer host. Keep the existing
stack name (`winget`) and the named volume keys when replacing its editor
contents. The stack builds rewinged 0.15.0 natively because its official image
is AMD64-only. The source archive is SHA256 pinned; Go caches and the resulting
binary persist across deployments. It exposes host port **8088** to container
port 8080. CloudPanel must proxy the whole domain to that port, preserving
`X-Forwarded-Host` and `X-Forwarded-Proto: https`, including `/installers/`.

1. In **Portainer → Stacks → winget → Editor**, replace the old YAML with
   `deploy/rewinged/stack-arm64.yml` and update the stack. Do not delete its
   volumes. Bootstrap defaults to the existing published `v0.1.0` and its
   verified `winget-manifests.zip` hash. It seeds official **Git.Git 2.56.0.4**
   manifests too. This repairs the reported `No matches / Error processing
   package dependencies` failure: WinGet requires dependencies in the same source.
   RefRail continues to declare Git as a dependency in both WinGet channels.
2. In **Portainer → My account → Access tokens**, create an access token and
   copy it once. Use an account permitted to inspect and update this stack in
   its environment. Store the token in GitHub as a secret, never in the stack
   editor or a public repository. Portainer must be reachable over HTTPS from
   the GitHub-hosted runner; a CloudPanel proxy for Portainer can provide this.
3. Get the stack ID from its Portainer details URL (`.../stacks/42?...` means
   `42`). This is the **stack ID**, not the environment/endpoint ID; the helper
   reads the latter from Portainer.
4. Under **RefRail → Settings → Secrets and variables → Actions**, add:

   | Kind | Name | Value |
   | --- | --- | --- |
   | Variable | `PRIVATE_WINGET_PORTAINER_URL` | Your Portainer HTTPS base URL, without `/api` |
   | Variable | `PRIVATE_WINGET_STACK_ID` | Positive numeric stack ID |
   | Variable (optional) | `PRIVATE_WINGET_STACK_NAME` | Defaults to `winget`; must match the existing stack |
   | Variable (optional) | `PRIVATE_WINGET_SOURCE_URL` | Defaults to `https://winget.noktabilisim.com/api` |
   | Secret | `PRIVATE_WINGET_PORTAINER_API_KEY` | Portainer access token |

5. Run **Distribute** from the updated `main` branch with the published tag
   (including existing `v0.1.0`), and select **private_winget**. The helper uses
   tools from the workflow revision and package metadata from the release tag,
   so old releases need no moved tag or new assets. The published manifest ZIP
   must match regenerated metadata, including the package ID and publisher.

The helper reads the stack and its YAML through Portainer's API, checks the stack
name/type/template, and sends the unchanged YAML back with merged environment
variables. Only `REFRAIL_TAG`, `REFRAIL_MANIFEST_SHA256` and
`REFRAIL_DEPLOY_REVISION` are managed. The revision forces init and server to
recreate even on a retry of the same tag. Historical versions remain available;
selecting an older tag does not remove or supersede a newer catalog version.
The key is sent only to Portainer, over HTTPS, with redirects refused. There is
no rewinged upload endpoint and no SSH or exposed Docker API requirement.

Installer caching can take a few minutes, especially on first Git download.
The job waits for both package manifests and checks the cached downloads' hashes,
including Git's x64/ARM64 installers. If verification fails, inspect
`manifests-init` and `rewinged` logs. A failed rewinged download can leave a
partial file in the `installers` volume; remove only the affected hash-named file
and restart the server before retrying. Do not clear the whole volume.
The pinned Git manifests, provenance and update instructions are in
[deploy/rewinged/git](../deploy/rewinged/git/README.md).

On Windows, after the private job succeeds:

```powershell
# Add once; administrator access may be required for source management.
winget source add --name noktabilisim --arg https://winget.noktabilisim.com/api --type Microsoft.Rest
winget source update --name noktabilisim
winget show --exact --id Git.Git --source noktabilisim
winget install --exact --id batuhanoztrk.RefRail --source noktabilisim
```

For later releases use `winget upgrade --exact --id batuhanoztrk.RefRail --source
noktabilisim`. Open a new terminal if PATH changes are not visible, then run
`git --version`, `refrail version --json` and `refrail doctor --repo <path>`.
The source API/download checks do not replace this Windows installation pilot.
This source permits anonymous reads; installers are accessible to anyone who
can reach the domain, despite the term “private” referring to the managed catalog.

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
- [WinGet dependency source requirements](https://github.com/microsoft/winget-pkgs/blob/master/doc/manifest/schema/1.10.0/installer.md#dependencies)
- [Portainer API access](https://docs.portainer.io/api/access)
- [Portainer stack update API implementation](https://github.com/portainer/portainer/blob/2.33.6/api/http/handler/stacks/stack_update.go)

GitHub Actions are pinned to commit hashes. WinGetCreate is pinned to version
1.12.13.0 and SHA-256. Vendored WinGet schemas are version 1.10.0; provenance
and the upstream license live under `tools/packaging/schemas`. Review and update
these pins explicitly when upgrading the release toolchain.
