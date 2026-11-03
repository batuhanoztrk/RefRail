# Pinned Git dependency

These three manifests are copied without changes from
[microsoft/winget-pkgs at 1999a32cfbf21f832643742522e1b1e8f651fd20](https://github.com/microsoft/winget-pkgs/tree/1999a32cfbf21f832643742522e1b1e8f651fd20/manifests/g/Git/Git/2.56.0.4).
Their MIT license is included in LICENSE. Git for Windows itself is GPL-2.0;
its installer, publisher, license and release URLs are preserved in the manifests.

WinGet resolves package dependencies from the same source as the parent package.
Seeding Git.Git avoids RefRail's dependency resolution failure in an otherwise
empty private source. Git has no further package dependencies in this pin.
rewinged 0.15.0 explicitly accepts manifest schema 1.12.0 using its 1.10.0 model
(the added `font` installer type is not used here).

The stack downloads these exact files from the pinned upstream commit and checks
all three SHA256 values before copying them into the persistent manifest volume.
The publisher verifies both architectures' Git installers as well as RefRail's.
When upgrading Git, update this directory, the stack's commit/version/checksums,
and the publisher's GIT_VERSION constant together. Run the distribution tests.
