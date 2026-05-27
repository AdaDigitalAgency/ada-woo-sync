# Contributing to wp-stage-sync

Thanks for your interest in contributing! This guide covers everything you need to get started.

## Prerequisites

- [Go 1.26+](https://go.dev/dl/)
- A Linux server (or VM) with WordPress sites to test against
- `rsync` and `mysql` client available in PATH

## Getting Started

```bash
git clone git@github.com:AdaDigitalAgency/wp-stage-sync.git
cd wp-stage-sync
go mod download
make build
```

This produces a `wp-stage-sync` binary in the project root.

## Project Structure

```
cmd/wp-stage-sync/   Entry point (CLI flags, unattended mode)
internal/
├── config/          JSON config persistence (~/.config/wp-stage-sync/)
├── db/              MySQL connection (socket fallback, IPv4 forcing)
├── discovery/       Apache/Nginx vhost scanning, site pairing
├── export/          Table export engine (HPOS orders, batched INSERTs)
├── guardrail/       Production safety checks (path + DB validation)
├── progress/        Logger interface (CLILogger, NopLogger)
├── selfupdate/      GitHub release update checker
├── sync/            File sync (rsync), post-processing (WP-CLI)
├── tui/             Bubbletea TUI (all interactive screens)
├── wpcli/           WP-CLI resolution and PHAR fallback
└── wpconfig/        wp-config.php parser
scripts/release.sh   Version bump, squash, tag, push
```

## Development Workflow

### Build and run locally

```bash
make build
./wp-stage-sync
```

The version is embedded at build time via `-ldflags`. Dev builds show `dev` as the version.

### Run on a server

Copy the binary to a Linux server with WordPress installs:

```bash
scp wp-stage-sync user@server:/usr/local/bin/
ssh user@server wp-stage-sync
```

### Code style

- `go vet` and `go build` must pass with no errors.
- Follow standard Go conventions. No linter is enforced, but keep it clean.
- No CGO — the binary must be statically linkable for Linux deploys.

## Making Changes

1. **Fork and branch.** Create a feature branch from `main`.
2. **Keep commits focused.** One logical change per commit.
3. **Test on a real server.** This tool touches live databases and files — there's no substitute for testing against actual WordPress installs.
4. **Update the changelog.** Add your changes to the `[Unreleased]` section of [CHANGELOG.md](CHANGELOG.md) following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions.

## Pull Requests

- Open a PR against `main`.
- Describe what you changed and why.
- Include the server environment you tested on (distro, WordPress version, WooCommerce version if relevant).

## Reporting Bugs

Open an issue with:

- The version (`wp-stage-sync --version`)
- Server OS and MySQL version
- WordPress and WooCommerce versions (if applicable)
- The error message or unexpected behavior
- Steps to reproduce

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
