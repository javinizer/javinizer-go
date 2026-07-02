# Homebrew tap — one-time setup

These steps create the `javinizer/homebrew-tap` repository and give the
`javinizer-go` CI permission to push the formula to it on each stable release.
Do this once; afterwards, the `update-homebrew-tap` job in
`.github/workflows/cli-release.yml` keeps `Formula/javinizer.rb` in sync
automatically.

## 1. Create the tap repo

Create an empty public repository named **`homebrew-tap`** under the
`javinizer` organization (no README, no license, no `.gitignore` — keep it
empty so the first CI push is clean):

https://github.com/organizations/javinizer/repositories/new

- Owner: `javinizer`
- Name: `homebrew-tap`
- Visibility: **Public** (Homebrew requires taps to be public for `brew install`)
- Initialize: leave all unchecked

## 2. Generate an SSH deploy key

```bash
ssh-keygen -t ed25519 -C "homebrew-tap deploy key" -f homebrew_tap_deploy_key -N ""
```

This produces two files:
- `homebrew_tap_deploy_key`      → private key (goes into a CI secret)
- `homebrew_tap_deploy_key.pub`  → public key (goes into the tap repo)

## 3. Add the public key as a write deploy key on the tap repo

Go to https://github.com/javinizer/homebrew-tap/settings/keys/new
- Title: `CI publish from javinizer-go`
- Key: paste `homebrew_tap_deploy_key.pub`
- **Allow write access: ✓** (required — CI commits the formula)

## 4. Add the private key as a secret in javinizer-go

Go to https://github.com/javinizer/javinizer-go/settings/secrets/actions/new
- Name: `HOMEBREW_TAP_DEPLOY_KEY`
- Secret: paste the contents of `homebrew_tap_deploy_key` (the private key)

## 5. Delete the local private key

```bash
rm homebrew_tap_deploy_key homebrew_tap_deploy_key.pub
```

## How users install

Once a stable (non-prerelease) `v1.0.0` ships and the CI job has run:

```bash
brew tap javinizer/homebrew-tap https://github.com/javinizer/homebrew-tap
brew install javinizer
brew upgrade javinizer   # updates to the latest stable release
```

## Notes

- The tap is only updated for **stable** releases. Prereleases (`v1.0.0-rc.*`)
  do not push to the tap, so `brew upgrade` never hands a user a release
  candidate unless they explicitly downgrade.
- The formula installs prebuilt binaries (CGO/SQLite is statically linked into
  each release asset), so Homebrew does not need to build from source or pull
  in a SQLite dependency.
- `javinizer upgrade` (self-upgrade) detects a Homebrew install by its Cellar
  path and tells the user to run `brew upgrade javinizer` instead, so the two
  channels never fight each other.
