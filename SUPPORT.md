# Support

This maintained fork supports users with active Beatport streaming subscriptions.

Standalone Beatsource support has been removed following the upstream change and Beatport's [migration guidance](https://support.beatport.com/hc/en-us/articles/46607140394004-Beatsource-Migration-FAQ). Use your migrated Beatport account and Beatport URLs. Old Beatsource IDs are not automatically converted, and local config, credentials, and downloaded files are left unchanged.

## Supported

- Installation, build, and release binary questions.
- Bugs in URL parsing, downloads, filenames, tags, and configuration.
- Diagnostics for subscription-tier quality mismatches, failed requests, and unclear error messages.
- Feature requests that preserve compatibility with existing config files and workflows.

## Not Supported

- Bypassing subscriptions, payment requirements, territorial restrictions, account limits, or other platform access controls.
- Sharing credentials, tokens, cookies, or private account data in issues.
- Requests for unsupported distribution targets before the maintained fork is stable.

## Useful Bug Reports

Include:

- BeatportDL version or commit hash.
- OS and architecture.
- Active subscription tier.
- Configured quality.
- URL type: track, release, playlist, chart, label, artist, or search query.
- Sanitized command output, config, and logs.

Remove usernames, passwords, tokens, cookies, account identifiers, and private local paths before posting.

## Common 400/403 Causes

- `400` on download requests often means the requested quality is not available for your Beatport subscription tier.
- `403` on login or token refresh usually means invalid credentials or stale cached credentials.
- `403` on track, release, or download requests can also mean territorial availability restrictions or an unsupported subscription tier for that resource.
