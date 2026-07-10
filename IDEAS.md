# Ideas

Future improvements that aren't scheduled yet — so they don't get lost.

## Prebuilt release binaries with goreleaser

The release flow is already automated end to end:

1. `make release VERSION=x.y.z` — validates the version, branch, and clean
   working tree, runs tests, creates and pushes the `vx.y.z` tag, and
   publishes a GitHub Release via `gh`.
2. Publishing the release fires `.github/workflows/update-homebrew-tap.yml`,
   which sends a `repository_dispatch` (`update-formula`) to
   `bshakr/homebrew-koh` with the tag and commit SHA; that repo updates
   `Formula/koh.rb` itself.

Caveat: the tap workflow only fires on *published GitHub Releases*, not on
tag pushes — v0.1.8 and v0.1.9 were tagged without releases, so the formula
had to be updated by hand for those. Releasing via `make release` (rather
than tagging directly) avoids this.

What [goreleaser](https://goreleaser.com) would still add: prebuilt
per-platform binaries attached to each GitHub Release, so `brew install koh`
(and direct downloads) no longer need a Go toolchain and become
near-instant. If it also takes over formula updates (`brews:` config), it
needs a fine-grained PAT with write access to `bshakr/homebrew-koh` stored
as a repo secret.

## Other

- Batch prune classification into two `git for-each-ref` calls instead of
  ~2 execs per worktree (`internal/git/prune.go` — noticeable with many
  worktrees).
- Extract one shared worktree-teardown helper for `cleanup` and `prune` so
  removal semantics can't drift between them.
- tmux window matching is by worktree basename only (`*|<name>`), so two
  repos sharing a worktree name in one tmux session can collide; missing
  windows are detected by error-string match (`internal/tmux/session.go`) —
  use a sentinel error and repo-qualified matching.
