# Desktop automatic updates for this fork

Automatic updates are disabled by default. Download this fork's installers from [cecilylove/easy-stock Releases](https://github.com/cecilylove/easy-stock/releases). The original author's OSS feed is rejected to prevent an upstream package from replacing fork features.

To enable updates, set `A_STOCK_UPDATE_FEED_URL` when building. The builder saves the validated URL in `resources/desktop-update-config.json`; packaged macOS and Windows apps read that value at runtime. A runtime environment value overrides it, and an explicitly empty value disables updates. URLs require HTTPS, except HTTP on loopback hosts for local testing; credentials, queries and fragments are rejected. Use a feed controlled by this fork's maintainer.

When enabled, the app checks 30 seconds after startup and every 12 hours. Downloads and restarts require a user action. Development builds do not install automatic updates.

## Data protection

Before installation, the app stops local services, flushes Electron sessions and backs up user data outside `userData`. Windows comparisons ignore case and resolve physical paths, including junctions, to reject a backup directory inside the source. Locked files are retried, then recorded as skipped in `manifest.json` if they remain locked. Only validated backups carrying the `easy-stock` application marker are automatically pruned; the latest three are retained. Unknown directories, links and pre-marker backups are preserved.

The research database normally lives at `userData/stock-research.db`. For the default data directory, an existing legacy research database is copied using SQLite's online backup API, including committed WAL content. The original is retained and an existing destination is never overwritten or merged. A custom isolated data directory does not import global history. If migration fails, the app continues using the original database and reports the deferred migration. A migration notice is saved in `stock-research-migration.json`.

`A_STOCK_RESEARCH_DB` explicitly selects an external research database. Research databases used by the app are backed up with a consistent SQLite snapshot; an external database is stored as `data/stock-research-external.db`, and the manifest records its source path. Failure to snapshot the research database prevents installation. Database migration/open failures in the backend prevent it from silently opening empty history.

## Release configuration

Push a tag matching `desktop/package.json`, or manually run the `Desktop Release` workflow with an existing tag. By default, CI builds macOS arm64/x64 and Windows x64 installers and publishes DMGs, the Windows installer and `SHA256SUMS.txt` to GitHub Releases. It does not require OSS credentials.

Optional signing secrets:

- macOS: `MAC_CSC_LINK`, `MAC_CSC_KEY_PASSWORD`, `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`.
- Windows: `WIN_CSC_LINK`, `WIN_CSC_KEY_PASSWORD`.

To publish an automatic update channel, configure repository variables `A_STOCK_UPDATE_FEED_URL` and `OSS_UPDATE_TARGET_URI` for your own public feed and bucket prefix, plus `OSS_REGION`/`OSS_ENDPOINT` as required by your bucket. Configure secrets `OSS_ACCESS_KEY_ID` and `OSS_ACCESS_KEY_SECRET` with write access to that prefix. The OSS publish step runs only when the feed, target and both access-key secrets are present. A configured feed also enables updater metadata verification; incomplete configuration does not publish a working update channel.

For that channel, CI merges/verifies metadata, uploads immutable versioned ZIP/EXE assets first, uploads `latest-mac.yml` and `latest.yml` last with `no-cache`, then probes public URLs. Keep every asset referenced by the channel metadata. The explicit local publishing command is:

```bash
bash desktop/scripts/publish-updater-oss.sh <asset-dir> <fork-owned-target-uri> <fork-owned-public-url>
```

Unsigned CI packages may be used for smoke testing. macOS automatic installation requires Developer ID signing and notarization; Windows distribution should use Authenticode. Unit tests do not replace target-platform packaging, installation and publication checks.
