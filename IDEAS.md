# Ideas

Future improvements that aren't scheduled yet — so they don't get lost.

## Release automation with goreleaser

Today a release is manual: tag `vX.Y.Z`, push, then hand-edit `tag:` and
`revision:` in `bshakr/homebrew-koh/Formula/koh.rb`. Replace this with
[goreleaser](https://goreleaser.com) triggered by a tag-push GitHub Action:

- builds prebuilt binaries per platform and attaches them to a GitHub Release
- rewrites the tap formula automatically (`brews:` config) — `brew install koh`
  no longer needs a Go toolchain and becomes near-instant
- needs a fine-grained PAT with write access to `bshakr/homebrew-koh`, stored
  as a repo secret

Interim smaller step if goreleaser feels heavy: a tag-triggered workflow that
just sed-updates `tag:`/`revision:` in the formula and pushes (~30 lines of
YAML, same PAT requirement), keeping the build-from-source formula.

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
