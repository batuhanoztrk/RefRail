# RefRail

Portable Git commit and branch policy checks, powered by Go and Lefthook.

[Türkçe kullanım kılavuzu](docs/tr.md) · [Contributing](CONTRIBUTING.md) · [MIT license](LICENSE)

RefRail helps teams use Conventional Commits and consistent branch names across .NET, Go, Node.js, Flutter, React Native, Kotlin, Swift, and other projects. It works with regular Git repositories, including Azure Repos, GitHub, and GitLab.

**Developers need Git and the platform bundle.** Node.js, npm, Go, Rust, and a preinstalled Lefthook are not required. The bundle includes a pinned Lefthook executable. Installation and daily checks work offline without administrator access. Windows requires the shell provided by Git for Windows.

## Package managers

Once the first release and package submissions are published:

```sh
brew install batuhanoztrk/tap/refrail
refrail install --repo "/path/to/project"
```

```powershell
winget install --exact --id batuhanoztrk.RefRail
refrail install --repo "C:\Projects\app"
```

For the managed Nokta Bilişim source:

```powershell
winget source add --name noktabilisim --arg https://winget.noktabilisim.com/api --type Microsoft.Rest
winget install --exact --id batuhanoztrk.RefRail --source noktabilisim
```

The private catalog must contain Git.Git too; its Portainer stack seeds this
dependency. See the [private source setup](docs/releasing.md#private-winget-one-time-portainer-setup).

Homebrew supports macOS and Linux, on Intel/AMD64 and ARM64. WinGet packages
target Windows x64 and ARM64 and declare Git for Windows as a dependency.
RefRail finds its complete bundle through the package manager's executable
link; `--bundle` remains available for explicit offline bundles.

After `brew upgrade` or `winget upgrade`, run `refrail update --repo <path>`
for each clone you want to upgrade. Existing clones retain their pinned bundle.
Before removing the CLI, use `refrail uninstall --repo <path>` for each clone
whose hooks you want removed. See the [publishing guide](docs/releasing.md)
for GitHub Actions, credentials, and first-release setup.

## Install after cloning

Extract the ZIP for your OS and architecture and enter the enclosed `refrail`
directory. From that bundle root:

```sh
./bin/refrail install --repo "/path/to/project" --bundle "."
./bin/refrail doctor --repo "/path/to/project"
```

Windows PowerShell:

```powershell
.\bin\refrail.exe install --repo "C:\Projects\app" --bundle "."
.\bin\refrail.exe doctor --repo "C:\Projects\app"
```

Each developer runs `install` once for each clone. Hook activation lives in Git metadata; it is not committed or inherited by cloning. Normal `git pull` operations do not require reinstalling. Repeating installation with the same bundle is safe.

RefRail does not change global Git settings, shell profiles, or application source files. Existing hooks or `core.hooksPath` produce a conflict instead of being overwritten. Worktrees of one clone share the same installation and baseline.

## Default policy

```text
feat(auth): add biometric login
fix: correct camera permission handling
feat(api)!: change the response format
revert: remove the experimental login flow

feat/add-login
fix/1234-camera-permission
hotfix/payment-error
```

- Commit types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`.
- Optional scope and `!`; descriptions may use any language. Header limit: 100 Unicode characters.
- The first physical line must be a valid header. Put a blank line before the body or footers. Body comments and `BREAKING CHANGE:` are supported.
- Raw, whitespace-cleaned, and comment-cleaned forms must all be valid. This avoids guessing Git's unobservable per-command `--cleanup` override. RefRail never rewrites messages.
- `fixup!`, `squash!`, and automatic `Revert "..."` messages are rejected. Genuine merge commits are exempt; normal commits titled `Merge ...` are not.
- Branches use an allowed prefix and a lowercase ASCII, hyphen-separated description. Commit types and `hotfix` are allowed branch prefixes. Ticket numbers are optional.
- `main`, `master`, and `develop` are exempt from branch naming; their new commits are checked. Branch prefixes do not constrain every commit's type.

## Project restrictions

Commit an optional `.refrail.json` to the application repository:

```json
{
  "commitTypes": ["feat", "fix", "docs", "chore", "revert"],
  "branchPrefixes": ["feat", "fix", "docs", "chore", "hotfix"],
  "maxHeaderLength": 72,
  "allowedScopes": ["auth", "camera", "api"],
  "requireScope": true,
  "requireTicket": true
}
```

Settings may only tighten the shared policy. Lists must be nonempty subsets where a shared allowlist exists; the header limit cannot increase. `allowedScopes` restricts scopes that are supplied; use `requireScope` to make scopes mandatory. Unknown fields, `null`, duplicate list entries, and attempts to relax shared rules fail. Project JSON never executes commands.

## Push validation

RefRail checks the **destination refs in the actual push**, including renamed destinations and multiple branches. New commits are checked again, so bypassing `commit-msg` alone does not bypass a normal push.

Installation records current HEAD and its ancestors as the legacy baseline, anchored under `refs/refrail/baseline`. Updates never advance it. New-branch pushes also exclude locally known history for the destination remote. First pushes and force pushes are supported. Branch deletions and tag updates are outside this phase's checks. Shallow or missing history fails with a fetch instruction; hooks never download history automatically.

## Languages

RefRail uses **Turkish when the device UI language is Turkish**, and **English otherwise**. macOS reads the first preferred `AppleLanguages` entry; Windows reads the current user's UI language. Linux uses `LC_ALL`, `LC_MESSAGES`, `LANGUAGE`, then `LANG`. Native lookup failures fall back to those variables, then English.

For CI, tests, or an explicit preference, set `REFRAIL_LANG=tr` or `REFRAIL_LANG=en`. The override is forwarded to child hooks. JSON keys, command names, and rule identifiers stay stable. Git, Lefthook, and operating-system diagnostics retain their own language.

If the bootstrap executable itself is missing, the shell wrapper provides a bilingual recovery message because RefRail cannot start to detect the device language.

Policy failures show the rejected commit header, the validation reason, and an example Git command. Supported terminals use red headings, yellow requirements, green examples, and dim rule identifiers. Redirected output stays plain; set a nonempty `NO_COLOR` to disable colors explicitly. `TERM=dumb` also disables colors. Windows consoles use colors when virtual terminal output is enabled.

## Update, rollback, and removal

Extract a new bundle and run:

```sh
./bin/refrail update --repo "/path/to/project" --bundle "."
./bin/refrail doctor --repo "/path/to/project"
```

Use `update` with an older bundle to roll back. Versions are pinned per clone. Bundles live under `os.UserConfigDir()/refrail/packages`, or `REFRAIL_HOME` when set. Updates and rollbacks refresh the managed wrappers to use the selected bundle's absolute executable path, preserving the installation baseline. If a write fails, already-replaced wrappers are restored. Old bundles are retained because other clones may still use them.

```sh
./bin/refrail uninstall --repo "/path/to/project"
```

Uninstall removes managed hooks and installation state, preserving shared bundles and project JSON. Filesystem or Git-ref failures roll back reversible steps. Modified hooks and unexpected metadata require inspection.

The unpublished prototype used `company-git-policy`. Uninstall an activated prototype using its original bundle before installing RefRail. Rename any committed `.company-git-policy.json` to `.refrail.json` after reviewing its rules. Old hooks are never silently adopted.

## Other commands

```sh
./bin/refrail version --json
./bin/refrail commit-msg --repo "/path/to/project" --file "/path/to/message.txt"
./bin/refrail check --repo "/path/to/project" --from BASE_SHA --to HEAD_SHA
```

`pre-push --remote origin` consumes Git's four-field ref updates on stdin. `hook-run` is an internal wrapper command. Exit codes: **0** success, **1** policy violation, **2** operational/configuration error. Git can return its own top-level failure code.

## Build and test

Maintainers need Go 1.27+ and Git:

```sh
go run ./tools/release --version v0.1.0
go test ./... -count=1
go vet ./...
```

For a native-only build, pass `--targets darwin/arm64`, `linux/amd64`, or the appropriate target. Real-hook tests discover the native bundle's Lefthook, accept `REFRAIL_TEST_LEFTHOOK`, or use Lefthook 2.1.17 from PATH. They are explicitly skipped when none is available.

Outputs are under `dist/v0.1.0`: extracted bundles, `refrail_v0.1.0_<os>_<arch>.zip`, and `SHA256SUMS`. Validators use `CGO_ENABLED=0`. Official Lefthook **2.1.17** binaries are verified against checksums pinned in this repository. Downloads are cached; runtime never downloads or compiles tools. Bundles include the MIT license and third-party notices.

SHA-256 detects corruption; it is not publisher authentication. Use a trusted distribution channel. Local commands do not create a public repository or publish a release.

Create an isolated working repository and bare remote:

```sh
go run ./tools/sandbox --bundle dist/v0.1.0/darwin-arm64 --out sandbox/refrail-demo
```

Choose a fresh output directory. The sandbox checks installation and hooks with no system Lefthook, Node.js, Go, or Rust in PATH. Results go to `report.json`.

## Limits

Local hooks can be disabled by the user; they are not server-side enforcement. Project policy is read from the working tree. Git clients that do not run hooks must be tested separately. Cross-building is not proof of execution on an OS; further platform and IDE pilots remain necessary.

## License

RefRail is distributed under the [MIT License](LICENSE). Bundled components retain their own licenses; see [third-party notices](THIRD_PARTY_NOTICES.md).
