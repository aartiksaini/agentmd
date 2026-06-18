# Compile and Build Guide

## Prerequisites

The build environment is handled by the `kerberos/base` Docker image, which includes:
- Go 1.24
- FFmpeg dev libraries (`libavcodec`, `libavutil`, `libswresample`) — required for CGo audio transcoding
- Build tools (`gcc`, `pkg-config`, `cmake`, etc.)

For local Go builds outside Docker, install FFmpeg dev headers manually:
```bash
sudo apt-get install -y libavcodec-dev libavutil-dev libswresample-dev
```

---

## Local Docker Build

Run from the `agent/` directory.

**AMD64 (x86_64):**
```bash
docker build --build-arg VERSION=1.0.2 -t ghcr.io/<your-github-username>/agent:1.0.2-amd64 .
```

**ARM64:**
```bash
docker build --build-arg VERSION=1.0.2 -t ghcr.io/<your-github-username>/agent:1.0.2-arm64 -f Dockerfile.arm64 .
```

**Multi-arch with buildx:**
```bash
docker buildx create --use --name multiarch   # one-time setup

docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --build-arg VERSION=1.0.2 \
  -t ghcr.io/<your-github-username>/agent:1.0.2 \
  --push .
```

Omitting `--build-arg VERSION` defaults to `0.0.0` (or the git tag if present).

---

## Local Go Compile Only

```bash
cd machinery
go mod download
go build -v ./...
go vet -v ./...
go test -v ./...
```

CGo is enabled by default. FFmpeg dev headers must be installed (see Prerequisites).

---

## GitHub Actions Workflows

### Workflow Overview

| Workflow | Trigger | Purpose |
|---|---|---|
| `go.yml` | Push / PR to `dev`, `develop`, `main`, `master` | Compiles Go, runs vet and tests |
| `pr-build.yml` | PR opened or updated | Builds Docker image for amd64 + arm64, uploads binaries as artifacts (no push) |
| `build-main.yml` | Push to `main` (i.e. merge of any PR) | Builds and pushes images to GHCR tagged with short SHA and `latest` |
| `nightly-build.yml` | Daily 10 PM UTC or manual dispatch | Builds and pushes nightly images to GHCR |
| `release-create.yml` | GitHub release created or manual dispatch | Builds versioned images, creates manifest and GitHub release |
| `release-bump.yml` | Manual dispatch | Auto-bumps version (major/minor/patch), then runs the full release pipeline |

### Dev → Main flow (recommended for day-to-day work)

```
Push to dev branch
    └── go.yml triggers → compiles, vets, tests Go code

Open PR from dev to main
    └── pr-build.yml triggers → test Docker build for amd64 + arm64
                              → uploads binary tarballs as artifacts

Merge PR to main
    └── build-main.yml triggers → builds amd64 + arm64 images
                                → pushes to GHCR as:
                                    ghcr.io/<owner>/agent:<short-sha>
                                    ghcr.io/<owner>/agent:latest
                                → creates multi-arch manifest
```

### When to Use Other Workflows

**Ship a versioned release:**
Go to **Actions → Bump release → Run workflow**, pick `patch`, `minor`, or `major`.
Auto-increments the git tag, builds amd64 + arm64, pushes versioned images, creates GitHub release with binary tarballs.

**Release a specific tag manually:**
Go to **Actions → Create a new release → Run workflow**, enter the tag (e.g. `1.0.2`).

### GitHub Container Registry Image Naming

Images are published to `ghcr.io/<github-owner>/...` — automatically derived from the repository owner at build time.

| Image | Tag pattern | Example |
|---|---|---|
| `ghcr.io/<owner>/agent-arch` | `arch-amd64-<version>` | `ghcr.io/aartiksaini/agent-arch:arch-amd64-1.0.2` |
| `ghcr.io/<owner>/agent-arch` | `arch-arm64-<version>` | `ghcr.io/aartiksaini/agent-arch:arch-arm64-1.0.2` |
| `ghcr.io/<owner>/agent` | `<version>` (multi-arch manifest) | `ghcr.io/aartiksaini/agent:1.0.2` |
| `ghcr.io/<owner>/agent` | `latest` | `ghcr.io/aartiksaini/agent:latest` |
| `ghcr.io/<owner>/agent-nightly` | `<short-sha>` | `ghcr.io/aartiksaini/agent-nightly:a3f9c12` |

No secrets configuration is needed — workflows authenticate using the built-in `GITHUB_TOKEN` with `packages: write` permission.

---

## Deploying to Edge

After a new image is published to GHCR, update the image reference in `edge_deployment/docker-compose.yml` or `edge_deployment/playbook.yml`:

```yaml
image: ghcr.io/aartiksaini/agent:1.0.2
```

Then re-run the Ansible playbook:

```bash
ansible-playbook -i inventory edge_deployment/playbook.yml
```

To pull from GHCR on the edge device, log in once with a GitHub Personal Access Token (PAT) that has `read:packages` scope:

```bash
echo <PAT> | docker login ghcr.io -u <github-username> --password-stdin
```

---

## Notes

- The `mulaw_to_aac.go` file uses CGo with FFmpeg. The `kerberos/base` build image includes the required headers, so all Docker-based builds work without extra steps.
- The `go.yml` CI workflow runs inside `kerberos/base:amd64-ddbe40e` to match the same FFmpeg library versions used in the Dockerfile.
- The `Dockerfile` is for AMD64; `Dockerfile.arm64` is for ARM64. Both use the same multi-stage build pattern: Go compile → Node UI build → Alpine runtime image.
