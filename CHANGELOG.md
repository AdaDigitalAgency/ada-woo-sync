# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.5.0] - 2026-05-27

### Added

- **Promote mode** (`--promote`): surgically promote specific themes, plugins, and mu-plugins from staging to production with automated backup and atomic restore on failure.
- **Restore mode** (`--restore`): restore any previous backup from the TUI with full item-level recovery.
- Backup engine with tar.gz archives, JSON metadata sidecars, and configurable retention (default: 5 backups per site).
- `p` (promote) and `r` (restore) keyboard shortcuts on the startup screen.
- `--promote` and `--restore` CLI flags for direct mode entry.
- `-s` / `--site` flag to target a specific site in unattended or promote mode. Use `--list` to see configured identifiers.
- `-l` / `--list` flag to list all configured sites with their live → staging mapping.
- `--delete --site <id>` to remove a saved site config and its backups (prompts for confirmation).
- `--reset` to wipe all saved site configs and start fresh (prompts for confirmation).
- `wp-content/` path guardrail for promote mode — aborts if live and stage resolve to the same directory.
- Global `settings.json` for backup retention configuration.
- Legacy config migration: flat `sites/{domain}.json` files are automatically migrated to `sites/{domain}/config.json` directory structure on startup.
- Landing page and promotional assets for wp-stage-sync website.
- Feature logo and interactive image lightbox to documentation website.
- `CHANGELOG.md` following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions.
- **Help System**: Added `-h` / `--help` CLI flag to print available commands, flags, and brief usage examples. Added an interactive help screen to the TUI (toggle with `h`).
- **Settings Screen**: Added a global settings screen to the TUI (accessible via `s` from the startup screen) to configure backup retention limits and automatic cache flushing. Settings are persisted to `~/.config/wp-stage-sync/settings.json`.
- `CONTRIBUTING.md` with development setup, project structure, and PR guidelines.

### Changed

- Config storage moved from `sites/{domain}.json` to `sites/{domain}/config.json` (directory-based, backward-compatible via auto-migration).

### Fixed

- Install script now works without root/sudo access.

## [0.4.2] - 2026-05-25

### Added

- Global navigation shortcuts: `q` to quit, `Esc` to go back, `h` for help.
- Dual-sided status bar footer showing shortcuts and version info.
- Backspace support for navigating to parent directory in file picker.
- Recursive back-navigation across all TUI steps via `Esc`.

### Changed

- Updated directory navigator legend to reflect new keybindings.

## [0.4.1] - 2026-05-25

### Fixed

- Directory selection ambiguity resolved by checking cursor position on Space key.

## [0.4.0] - 2026-05-25

### Added

- Interactive directory picker for selecting live/stage webroots (replaces manual text input).

### Changed

- Replaced manual path text inputs with a custom directories-only file browser.

### Removed

- Unused `bubbles/filepicker` dependency from `go.mod`.

## [0.3.1] - 2026-05-25

### Added

- WooCommerce auto-detection (checks `wp-content/plugins/woocommerce` folder first, then DB tables).
- Customer data anonymization option in sync parameters.
- Anonymize WooCommerce order addresses (HPOS `wc_order_addresses` table).

### Changed

- WooCommerce-specific sync parameters are now hidden when WooCommerce is not detected.

### Fixed

- Non-existent default exclude directories no longer shown in the TUI exclude list.

## [0.3.0] - 2026-05-25

### Added

- Nginx vhost discovery alongside Apache.
- WP-CLI PHAR auto-download fallback when `wp` is not in PATH.
- Support for `DB_HOST` formats with port and socket.
- HPOS (High-Performance Order Storage) detection.
- RHEL/AlmaLinux Apache config paths (`/etc/httpd`).
- Per-site configuration (multiple saved site configs).
- Rsync exclude selection step in TUI.

### Changed

- Renamed binary from `wp-sync` to `wp-stage-sync`.
- Standardized TUI formatting and updated ASCII banner art.
- ASCII banner now shown on the paths step too.

## [0.2.2] - 2026-05-25

### Added

- `archive-master-db` directory to default rsync exclusions.

## [0.2.1] - 2026-05-24

### Added

- Sync duration displayed on TUI done screen and terminal exit message.

## [0.2.0] - 2026-05-24

### Added

- PgUp/PgDn/Home/End keyboard navigation for table selection list.

## [0.1.9] - 2026-05-24

### Fixed

- Built-in table modes now always take precedence over saved config.
- `export.DefaultTableMode` is the single source of truth for table modes.

## [0.1.8] - 2026-05-24

### Fixed

- Correct default modes now displayed for built-in custom/structure-only tables.

## [0.1.7] - 2026-05-24

### Added

- Filter `woocommerce_order_items` and `woocommerce_order_itemmeta` by order IDs.
- `mainwp_child_changes_logs` and `mainwp_child_changes_logs_meta` as structure-only defaults.
- Filter `yith_ywpar_points_log` by `order_id` or `user_id`.

## [0.1.6] - 2026-05-24

### Added

- Completed steps summary on the done screen.
- Sync completion timestamp printed after exiting TUI.
- Sync completion timestamp in unattended mode output.

### Fixed

- Silenced rsync, chown, and WP-CLI subprocess output.

## [0.1.5] - 2026-05-24

### Added

- Jetpack safe mode automatically enabled on staging if Jetpack is installed.

## [0.1.4] - 2026-05-24

### Fixed

- Skip Elementor URL replace if plugin is not installed.
- Clear cached version state after successful `--update`.

## [0.1.3] - 2026-05-24

### Added

- Update notice in TUI footer when a new version is available.

### Changed

- Replaced `.last-update-check` file with `.internal` JSON state file.

## [0.1.2] - 2026-05-24

### Changed

- Improved release script: squash commits, combine messages, auto-push.

## [0.1.1] - 2026-05-24

### Added

- Auto-migrate legacy `~/.wp-sync.json` config to new `~/.config/wp-sync/config.json` location.

### Changed

- Consolidated all config files under `~/.config/wp-sync/` directory.

## [0.1.0] - 2026-05-24

### Added

- Progress reporting for the full sync process.
- `progress.Logger` interface with `CLILogger` (timestamped stderr) and `NopLogger`.
- TUI mode: animated spinner, step checklist with checkmarks, detail line, progress bar.
- Unattended mode: timestamped step/detail/progress output to stderr.
- Per-table progress reporting during export with row counts.
- Per-group statement progress during import.
- File sync and post-processing substep reporting.

## [0.0.8] - 2026-05-23

### Added

- Prioritize wp-config unix socket for database connections.
- Force IPv4 (`127.0.0.1`) for localhost TCP connections.

## [0.0.7] - 2026-05-23

### Changed

- Disabled CGO for fully static Linux builds.
- Added quick one-liner install command to README.

## [0.0.6] - 2026-05-23

### Fixed

- Self-update: use `go-selfupdate` naming convention for release assets (`{binary}_{GOOS}_{GOARCH}`).

## [0.0.5] - 2026-05-23

### Added

- `--allow-root` flag passed to all WP-CLI commands.

### Fixed

- Connection reset on large `wp_posts` INSERTs: batch by byte size (8 MB cap) instead of 1000-row count.
- Set `GLOBAL max_allowed_packet=256MB` before import to handle large `post_content`.

## [0.0.4] - 2026-05-23

### Added

- Auto-pair live/stage sites via `discovery.PairSites()` (matches `example.com` ↔ `stage.example.com`).
- Selectable site pair list with arrow key navigation.
- "Custom paths…" option falls back to manual text input.

### Changed

- Updated Go version to 1.26 in release workflow.

## [0.0.3] - 2026-05-23

### Changed

- Updated workflow dependencies (`setup-go` v6, `action-gh-release` v3).
- Standardized release binary naming convention.

### Fixed

- Node.js 24 deprecation warnings in CI.

## [0.0.2] - 2026-05-23

### Added

- Makefile with `release-patch`, `release-minor`, `release-major` targets.

### Fixed

- Release script.

## [0.0.1] - 2026-05-23

### Added

- Initial release: WP/WooCommerce staging sync TUI application.
- Go CLI with bubbletea TUI and unattended mode (`-u` flag).
- Apache2 vhost and filesystem auto-discovery for WordPress installs.
- `wp-config.php` parser for DB credentials and table prefix.
- DB connection with root socket fallback to wp-config credentials.
- Export engine: filtered HPOS orders, safe user calculation, batched INSERTs.
- Import with foreign key check handling.
- Rsync file sync.
- WP-CLI post-processing (search-replace, flush).
- Production safety guardrails (path and DB identity validation).
- JSON config persistence (`~/.wp-sync.json`).
- Self-update mechanism via `--update` flag using `go-selfupdate`.
- `--version` / `-v` flag.
- GitHub Actions release workflow (linux/amd64 + arm64, checksums).
