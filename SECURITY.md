# Security Policy

## Supported Versions

Security fixes target the maintained fork's current default branch and latest
stable release. Update older installations before reporting a suspected issue.

## Reporting a Vulnerability

Use this repository's **Security > Report a vulnerability** option when it is
available. Do not post passwords, tokens, cookies, signed download URLs, private
music files, or identifying local paths in a public issue. If private reporting
is unavailable, open a minimal issue asking the maintainer for a private contact
channel without including exploit details or secrets.

Include the BeatportDL version, operating system and architecture, affected
configuration options, a credential-free reproduction, expected behavior, and
observed impact. Use synthetic fixtures rather than real account credentials.

## Security Boundaries

- Catalog metadata must not escape the selected download directory.
- Configuration, token caches, and error logs must use trusted locations and
  private files. Untrusted links, writable shared parents, and unsafe ACLs must
  fail explicitly, not trigger fallback credential writes.
- Downloads and remux output must be staged safely and published only after
  validation. Preserve existing files on failure or cancellation.
- Native format mismatches must return errors rather than crash the process.
- Logs must not disclose account secrets or signed URL parameters.
- Dependency inputs and GitHub actions must be pinned to reviewed identities;
  build jobs must not receive release publication authority.

## Scope

This is a local CLI, not a listening service. Processes running as the same user
and system administrators are trusted. Review shared-directory, symlink, ACL,
native parser, dependency, and release-pipeline boundaries without assuming that
an attacker already controls the operator's account or repository maintainers.
Do not contact Beatport or use live credentials while reproducing a report
without separate authorization.
