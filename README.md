# ADA WooCommerce Sync

A TUI (Terminal User Interface) tool to synchronize a live WordPress/WooCommerce site to a staging environment. Built for HPOS-enabled stores.

## What it does

- Copies your production WordPress database to staging with **filtered order data** — only the N orders you specify, not the entire order history
- Keeps all non-customer users (admins, editors, shop managers) intact
- Syncs `wp-content/` via rsync
- Runs search-replace for domain URLs automatically
- **Never writes to production** — read-only access to the live database, all mutations target staging only

## Requirements

- Go 1.21+ (build only)
- Linux server with:
  - MySQL/MariaDB
  - rsync
  - WP-CLI
  - Apache2 (optional, used for auto-discovery)

## Install

### From source

```bash
go install github.com/AdaDigitalAgency/ada-woo-sync/cmd/wp-sync@latest
```

### From releases

Download the latest binary from [Releases](https://github.com/AdaDigitalAgency/ada-woo-sync/releases) and place it in your `$PATH`.

## Usage

### Interactive mode (TUI)

```bash
wp-sync
```

Walks you through:

1. **Path selection** — auto-discovers WordPress installs from Apache2 vhost configs and filesystem
2. **Credential extraction** — parses `wp-config.php` automatically
3. **Order count & preference** — how many orders to copy (Last N or First N)
4. **Table selector** — choose sync mode per table (Structure & Data / Structure Only / Ignore / Custom Rule)
5. **Confirm & sync**

Settings are saved to `~/.wp-sync.json` for reuse.

### Unattended mode

```bash
wp-sync -u
```

Skips the TUI, reads saved config, and runs the sync immediately. Useful for cron jobs or scripts.

## How it works

### Database export (Step 0)

1. **Target orders** — queries `wc_orders` for the N most recent (or oldest) order IDs
2. **Safe users** — keeps all non-customer users unconditionally, plus customers linked to target orders
3. **Filtered tables** — HPOS tables (`wc_orders`, `wc_order_addresses`, `wc_orders_meta`, etc.) and WooCommerce lookup tables (`wc_order_stats`, `wc_order_product_lookup`, `wc_order_tax_lookup`, `wc_order_coupon_lookup`) are exported with `WHERE order_id IN (...)` filters
4. **Structure-only tables** — `woocommerce_sessions` and `actionscheduler_*` get schema only (no data)
5. **Base tables** — everything else copies as-is

### Database import (Step 1)

Drops all staging tables, imports with `FOREIGN_KEY_CHECKS=0`.

### File sync (Step 2)

```
rsync -av --delete --exclude='cache' --exclude='ewww' \
  --exclude='critical-css' --exclude='litespeed' \
  /home/{domain}/wp-content/ /home/stage.{domain}/wp-content/
```

Ownership is detected from the staging webroot and applied via `chown -R`. Falls back to `www-data:www-data`.

### Post-processing (Step 3)

```bash
wp search-replace https://{domain} https://stage.{domain} --all-tables
wp elementor replace-urls https://{domain} https://stage.{domain}
wp cache flush
```

## Safety

The tool has built-in production guardrails:

- **Read-only on production** — the live database connection only runs `SELECT` and `SHOW` queries. Zero `INSERT`, `UPDATE`, `DELETE`, or `DROP` statements touch production.
- **Path validation** — aborts if live and stage paths resolve to the same directory
- **Database validation** — aborts if live and stage point to the same database name + host
- **Staging-only mutations** — all `DROP TABLE`, `INSERT INTO`, rsync `--delete`, `chown`, and WP-CLI commands target the staging environment exclusively

## Project structure

```
cmd/wp-sync/main.go          Entry point, CLI flags, unattended orchestration
internal/
├── config/config.go          JSON config persistence (~/.wp-sync.json)
├── db/db.go                  DB connection (root socket → wp-config fallback)
├── discovery/discovery.go    Apache2 vhost + filesystem auto-discovery
├── export/export.go          Core export engine (orders, users, HPOS, base)
├── guardrail/guardrail.go    Production safety checks
├── sync/sync.go              Import, rsync, WP-CLI post-processing
├── tui/tui.go                Bubbletea interactive wizard
└── wpconfig/wpconfig.go      wp-config.php parser
```

## WooCommerce compatibility

- **HPOS**: Required. The tool queries `wc_orders` (not `wp_posts`) for order data.
- **Backwards compatibility**: Must be disabled. Legacy `wp_posts`-based order tables are not filtered.

## License

[MIT](LICENSE)
