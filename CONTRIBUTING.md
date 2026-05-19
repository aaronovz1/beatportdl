# Contributing

Thanks for helping keep this maintained fork healthy.

## Scope

This fork aims to stay compatible with the original `unspok3n/beatportdl` behavior while accepting maintenance fixes, build improvements, documentation updates, and carefully scoped features.

Do not submit changes that bypass subscriptions, territorial restrictions, account limits, or other platform access controls. BeatportDL requires an active Beatport or Beatsource streaming plan.

## Development

Run the pure Go checks before opening a pull request:

```shell
go test ./config ./internal/beatport ./internal/validator
```

Run the full suite when TagLib headers and libraries are installed:

```shell
go test ./...
```

If `taglib/tag_c.h` is missing, install TagLib development headers or provide the correct include and library paths through the Makefile environment variables.

## Pull Requests

- Keep changes focused and explain the user-visible behavior.
- Add tests for parsing, naming, config defaults, or other logic that can be tested without live credentials.
- For TagLib or download behavior, include a manual smoke-test note with sanitized logs.
- Preserve backwards compatibility for existing config files unless the PR explicitly documents a migration.
- Reference upstream issues or pull requests when adopting fixes from the original project.

## Release Branches

Maintenance releases use patch versions such as `v1.0.8`. New CLI features should wait for a minor release such as `v1.1.0`.
