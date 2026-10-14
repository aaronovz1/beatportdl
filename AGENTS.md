# Local Testing Workflow

After implementing and verifying BeatportDL code changes, always build a fresh
macOS arm64 release-style binary from the current working tree and install it at
`$HOME/beatportdl-darwin-arm64` before handing the work back for testing.

Verify the installed binary matches the build artifact and that its signature is
valid. Preserve the user's config, credentials, and other home-directory copies.
