# Releasing Hody

A release is one tag. Pushing `v0.1.0` makes GitHub Actions
(`.github/workflows/release.yml`) run the tests, then:

- build `hody` for macOS, Linux and Windows (Intel and ARM) with
  GoReleaser (`.goreleaser.yaml`), and publish them with `checksums.txt`
  and a changelog on the GitHub Releases page;
- push a multi-arch image (`linux/amd64`, `linux/arm64`) to
  `ghcr.io/astro-dally/hody`, with an SBOM and build provenance;
- if set up below: update the Homebrew cask and push the image to Docker
  Hub too.

Nothing is built on your laptop.

## One-time setup

Everything happens in the browser. You never paste a token into a chat,
a file or a commit: only into GitHub's secret settings.

### Homebrew (makes `brew install astro-dally/tap/hody` work)

1. Create a **public** repository named exactly `homebrew-tap` under
   `astro-dally`, with a README (so it is not empty).
2. Create a token that can write only to that repository: GitHub →
   Settings → Developer settings → Personal access tokens → Fine-grained
   tokens → Generate new token. Repository access: *Only select
   repositories* → `homebrew-tap`. Permissions: *Contents: Read and
   write*. Set an expiry date you will remember.
3. In `Hody-AI`: Settings → Secrets and variables → Actions → *New
   repository secret*, name `HOMEBREW_TAP_TOKEN`, value the token.

Without the secret, releases still work; the Homebrew step is skipped.

### Docker Hub (optional; GitHub's registry needs no setup)

1. On hub.docker.com: Account settings → Personal access tokens → create
   one with *Read & Write*.
2. In `Hody-AI`: Settings → Secrets and variables → Actions:
   - **Variables** tab: `DOCKERHUB_USERNAME` = your Docker Hub username;
   - **Secrets** tab: `DOCKERHUB_TOKEN` = the token.

Without the variable, the image goes to `ghcr.io` only.

### After the first release

- The image on `ghcr.io` starts private. Make it public: your GitHub
  profile → Packages → `hody` → Package settings → Change visibility →
  Public.
- Turn on Settings → Code security → *Private vulnerability reporting*,
  which `.github/SECURITY.md` points to, *Dependabot alerts* and
  *Dependabot security updates* (pull requests that fix vulnerable
  dependencies as soon as an advisory appears, between the weekly
  updates).

## Cutting a release

1. Make sure CI is green on `main` (the Actions tab, or the README badge).
2. Update `CHANGELOG.md`: the version, the date, what changed. Commit and
   push it.
3. Tag and push the tag (on your Mac):

   ```sh
   cd ~/Hody-AI
   git pull
   git tag -a v0.1.0 -m "Hody v0.1.0"
   git push origin v0.1.0
   ```

4. Watch Actions → *Release*. When it is green, check the release page,
   `brew install astro-dally/tap/hody`, and
   `docker run --rm ghcr.io/astro-dally/hody:0.1.0 version`.

A tag with a hyphen (`v0.2.0-rc.1`) is published as a pre-release and
does not move the image's `latest` tag.

## If a release fails

If the release page already has downloads, people may have fetched them and
checked them against `checksums.txt`, so never move that version to other
code: fix the problem on `main` and release the next patch version (v0.1.0
failed at the Homebrew step after publishing, so the fix shipped as
v0.1.1).

If it failed before anything was published, fix the problem on `main`,
then move the tag to the fixed commit:

```sh
git push --delete origin v0.1.0   # remove the tag on GitHub
git tag -d v0.1.0                 # and on your Mac
# fix, commit, push; then tag and push again as above
```

If the GitHub release page was already created, delete it there too
(Releases → the release → Delete) before pushing the tag again.

## Testing the release build without publishing

With GoReleaser installed (`brew install goreleaser`):

```sh
goreleaser check
goreleaser release --snapshot --clean   # builds everything into dist/, publishes nothing
```
