# Scoop bucket — one-time setup

These steps created the `javinizer/scoop-javinizer` bucket repository and gave
the `javinizer-go` CI permission to push the manifest to it on each stable
release. This is a reference of what was done; it only needs running once.

## 1. Create the bucket repo

Created an empty public repository named **`scoop-javinizer`** under the
`javinizer` organization (no README, no license, no `.gitignore`):

https://github.com/organizations/javinizer/repositories/new

- Owner: `javinizer`
- Name: `scoop-javinizer`
- Visibility: **Public** (Scoop requires buckets to be public)
- Initialize: leave all unchecked

## 2. Generate an SSH deploy key

```bash
ssh-keygen -t ed25519 -C "scoop-javinizer deploy key" -f scoop_bucket_deploy_key -N ""
```

This produces two files:
- `scoop_bucket_deploy_key`      → private key (goes into a CI secret)
- `scoop_bucket_deploy_key.pub`  → public key (goes into the bucket repo)

## 3. Add the public key as a write deploy key on the bucket repo

Go to https://github.com/javinizer/scoop-javinizer/settings/keys/new
- Title: `CI publish from javinizer-go`
- Key: paste `scoop_bucket_deploy_key.pub`
- **Allow write access: ✓** (required — CI commits the manifest)

## 4. Add the private key as a secret in javinizer-go

Go to https://github.com/javinizer/javinizer-go/settings/secrets/actions/new
- Name: `SCOOP_BUCKET_DEPLOY_KEY`
- Secret: paste the contents of `scoop_bucket_deploy_key` (the private key)

## 5. Delete the local private key

```bash
rm scoop_bucket_deploy_key scoop_bucket_deploy_key.pub
```

## How users install (Windows, Scoop)

Once a stable (non-prerelease) `v1.0.0` ships and the CI job has run:

```powershell
scoop bucket add javinizer https://github.com/javinizer/scoop-javinizer
scoop install javinizer
scoop update javinizer   # updates to the latest stable release
```

## Notes

- The bucket is only updated for **stable** releases. Prereleases
  (`v1.0.0-rc.*`) do not push to the bucket, so `scoop update` never hands a
  user a release candidate.
- The manifest (`bucket/javinizer.json`) installs the prebuilt
  `javinizer-windows-amd64.exe` and shims it as `javinizer`. CGO/SQLite is
  statically linked into the binary, so no separate runtime is required.
- The manifest includes `checkver` (stable-only regex) and `autoupdate` blocks
  so it is well-formed for Scoop tooling and self-documenting, even though CI
  also writes concrete version + hash per release.
- `javinizer upgrade` (self-upgrade) detects a Scoop install by its apps path
  and tells the user to run `scoop update javinizer` instead, so the two
  channels never fight each other.
