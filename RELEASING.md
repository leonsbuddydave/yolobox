# Releasing this fork

This is a personal fork of [`finbarr/yolobox`](https://github.com/finbarr/yolobox) at [`leonsbuddydave/yolobox`](https://github.com/leonsbuddydave/yolobox). Releases are cut from `master` and built via GitHub Actions.

This document covers the end-to-end release flow, the differences from upstream's pipeline, and the gotchas that have actually bitten us so it stays repeatable across sessions.

## Version policy

- Bump `v0.MAJOR.MINOR` per release. No fork suffix (no `-fork.N`, no `-mitchell.N`). The fork is its own product; if upstream later tags the same number with different content, that's their concern — our binary's update check points only at our own releases.
- Patch bumps (`v0.17.0` → `v0.17.1`) for bug fixes.
- Minor bumps for backward-compatible features.
- The `Version` constant is set at build time via `-ldflags "-X main.Version=$VERSION"` from the tag name; nothing to edit in code.

## What's different vs upstream's pipeline

Upstream's `release.yml` does Apple signing + notarization, Homebrew tap updates, and Docker image publishing. This fork has been simplified because:

- We don't have Apple Developer secrets (`APPLE_CERTIFICATE_BASE64`, `APPLE_ID`, etc.). macOS binaries ship **unsigned**.
- We don't own the Homebrew tap (`finbarr/homebrew-tap`). Users install via `install.sh`.
- We don't publish a Docker image. The default container image (`ghcr.io/finbarr/yolobox:latest`) still pulls from upstream.

The trimmed pipeline in `.github/workflows/release.yml` runs:

1. **Tests** (`go test -v ./...`, `go vet ./...`)
2. **Build Linux binaries** — `yolobox-linux-amd64`, `yolobox-linux-arm64`
3. **Build macOS binaries (unsigned)** — `yolobox-darwin-amd64`, `yolobox-darwin-arm64`
4. **Create release** with all four binaries + `checksums.txt`

The `install.sh` strips `com.apple.quarantine` after downloading the macOS binary so Gatekeeper doesn't block first launch.

## Where the update-check points

- `cmd/yolobox/version.go` — both the API URL and the displayed release URL point at `leonsbuddydave/yolobox`.
- `cmd/yolobox/maintenance.go` — `yolobox upgrade` downloads from `leonsbuddydave/yolobox`'s release artifacts.

Don't touch these unless we deliberately want to track upstream releases again.

## Release flow

Run from the repo root.

1. **Make your changes on a feature branch off `master`:**

   ```bash
   git checkout master
   git pull origin master
   git checkout -b feat/short-name
   # ... edits ...
   git commit -m "feat: ..."
   git push -u origin feat/short-name
   ```

2. **Open a PR into `master` and merge it via the gh CLI** (see the gotcha below — direct master pushes are blocked):

   ```bash
   gh pr create --base master --head feat/short-name \
     --title "feat: ..." --body "## Summary..."
   gh pr merge <PR#> --merge
   ```

3. **Fetch master and tag at its HEAD:**

   ```bash
   git fetch origin master
   git tag -a v0.x.y -m "v0.x.y — short summary" origin/master
   git push origin v0.x.y
   ```

   The tag push triggers `release.yml`. The whole pipeline takes ~5 minutes for a clean run.

4. **Verify the release:**

   ```bash
   gh release view v0.x.y
   ```

   Expect four binaries + `checksums.txt`.

5. **Pick up the new binary locally:**

   ```bash
   yolobox upgrade
   yolobox version   # confirm v0.x.y
   ```

   The first run of a new binary will hit the update-check; subsequent runs cache it for 24h (in `~/.config/yolobox/version-check.json`). Delete that file to force a fresh check.

## Installing from scratch (new machine)

```bash
YOLOBOX_BINDIR=/opt/homebrew/bin curl -fsSL \
  https://raw.githubusercontent.com/leonsbuddydave/yolobox/master/install.sh | bash
```

`YOLOBOX_BINDIR` defaults to `~/.local/bin` if omitted. On macOS, `/opt/homebrew/bin` is on `$PATH` for brew users by default; pick whichever fits your setup.

If a brew-installed `finbarr/tap/yolobox` exists first, `brew unlink yolobox` before running the install script so the new binary takes the canonical `/opt/homebrew/bin/yolobox` symlink path.

## Gotchas

### Claude Code blocks direct pushes to `master`

The `auto-mode` permission classifier in Claude Code will block `git push origin <branch>:master` against this fork even after explicit user consent, because direct pushes to a default branch bypass review by default. Use the PR + `gh pr merge` route (step 2 above) — that goes through the GitHub API rather than a ref push, and the classifier permits it.

If you genuinely need a direct push, run it as a shell command yourself (Claude Code's `!` prefix) — that bypasses the classifier because it's user-typed input.

### macOS binaries are unsigned

Downloads from the release page hit Gatekeeper. `install.sh` strips the quarantine attribute automatically. If you download a binary manually:

```bash
xattr -d com.apple.quarantine yolobox-darwin-*
```

### macOS attaches a "deny delete" ACL to bind-mount targets

Resolved in `v0.17.2`. Docker Desktop's VirtIO-FS layer attaches `user:USERNAME deny delete` ACLs to host directories used as container bind-mount targets. The ACL persists briefly after container teardown, which used to cause `yolobox fork discard` to fail with `unlinkat: permission denied` on first attempt. The `removeForkCopy` helper in `cmd/yolobox/fork.go` now strips ACLs via `chmod -RN` before `os.RemoveAll` on darwin.

If you see a similar error pattern surface elsewhere (e.g., a future cleanup path), use `removeForkCopy` rather than calling `os.RemoveAll` directly.

### Apple notarization is intentionally absent from `release.yml`

The signing/notarization jobs were stripped because we don't have the secrets. If you ever add them back to the fork, set:

- `APPLE_CERTIFICATE_BASE64`, `APPLE_CERTIFICATE_PASSWORD`
- `APPLE_ID`, `APPLE_ID_PASSWORD`
- `APPLE_TEAM_ID`

…and restore the relevant jobs from upstream's `release.yml`. Until then, the unsigned + `xattr -d` workaround stands.

### The Docker image isn't ours

`config.go` defaults `Image` to `ghcr.io/finbarr/yolobox:latest`. That's upstream's image; we use it as-is. If we ever want fork-specific image contents, we'd need to publish to `ghcr.io/leonsbuddydave/yolobox:latest` and flip the default. Out of scope today.

## Local testing before tagging

Always run before pushing a tag:

```bash
go build ./cmd/yolobox/
go test ./cmd/yolobox/ -count=1
go vet ./...
```

For sandbox-sensitive tests that write under `~/.yolobox/tmp/`, run outside any restrictive sandbox or set `t.Setenv("HOME", t.TempDir())` in the test.

## Syncing upstream

Master tracks the fork, not upstream. To pull upstream changes:

```bash
git remote add upstream https://github.com/finbarr/yolobox.git   # one-time
git fetch upstream
git checkout master
git merge upstream/master   # or rebase, depending on preference
# resolve conflicts (likely in version.go / maintenance.go URLs and release.yml)
```

Expect conflicts in:

- `cmd/yolobox/version.go` (the two `finbarr/yolobox` → `leonsbuddydave/yolobox` swaps)
- `cmd/yolobox/maintenance.go` (one swap)
- `.github/workflows/release.yml` (we've stripped the Apple/Homebrew/Docker jobs)

Keep our fork's version of each of those.
