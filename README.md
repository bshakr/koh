# koh

## git worktree tmux automation

`koh` is a CLI tool written in Go that creates git worktrees and sets up a configurable development environment with a single command.
![screenshot](./assets/screenshot.png)

## What it does

### `koh new <worktree-name>`

Creates a new development environment:

1. Creates a new git worktree in `.koh/<worktree-name>`
2. Opens a new tmux window with dynamically configured panes:
   - **First pane**: Runs your setup script (e.g., `./bin/setup`)
   - **Additional panes**: Runs any commands you configure (e.g., dev server, editor, etc.)

### `koh switch <worktree-name>`

Jumps to the tmux window for an existing worktree. If the window no longer exists (e.g. after a tmux restart), it's recreated with your configured panes. Like `koh new`, it must be run from inside a tmux session.

### `koh cleanup <worktree-name>`

Cleans up after you're done:

1. Force-removes the git worktree from `.koh/<worktree-name>` — **uncommitted changes are discarded**
2. Closes the tmux window for the worktree

### `koh prune`

Removes worktrees that are safe to clean up in bulk. A worktree is considered prunable when one or more of these apply:

- **`merged`** — the branch is fully merged into the default branch
- **`gone-from-remote`** — the upstream was deleted (covers squash-merged PRs)
- **`gone`** — the worktree directory has been removed on disk

`koh list` displays the same labels next to each worktree so you can see why something would be pruned.

By default `koh prune` opens an interactive picker with prunable worktrees pre-checked. The current worktree is never pruned — not even with `--yes` — and worktrees with uncommitted changes are skipped rather than force-removed (use `koh cleanup <name>` for those).

## Prerequisites

- [git](https://git-scm.com/)
- [tmux](https://github.com/tmux/tmux) — `koh new` and `koh switch` must be run from inside a tmux session
- macOS or Linux — `koh cleanup` and `koh prune` are not supported on Windows
- [Go](https://go.dev/) 1.24+ if building from source (the Homebrew tap also builds from source, so it installs Go as a build dependency)
- A setup script in your repository (optional, configurable via `koh init`)

## Installation

### Homebrew (recommended)

```bash
brew tap bshakr/koh
brew install koh
```

### Using Go

```bash
go install github.com/bshakr/koh@latest
```

### Building from source

```bash
# Clone the repository
git clone https://github.com/bshakr/koh.git
cd koh

# Or use go directly
go build -o koh
```

### Shell completions

`koh` ships with completion scripts for bash, zsh, fish, and PowerShell via `koh completion <shell>`. For example:

```bash
# zsh
koh completion zsh > "${fpath[1]}/_koh"

# bash
koh completion bash > /usr/local/etc/bash_completion.d/koh

# fish
koh completion fish > ~/.config/fish/completions/koh.fish
```

Run `koh completion <shell> --help` for shell-specific setup instructions.

## Usage

### First time setup

Navigate to your git repository and run:

```bash
koh init
```

This will guide you through setting up your configuration (setup script path and pane commands).

### Creating a new worktree session

```bash
koh new <worktree-name>
```

Example:

```bash
koh new feature-auth
```

This will:

- Create a worktree at `.koh/feature-auth`
- Set up your configured tmux environment with panes running your specified commands

If the setup script doesn't exist in the new worktree yet (e.g. it isn't committed), `koh new` copies it over from the main repository automatically.

### Switching between worktrees

```bash
koh switch <worktree-name>
```

Or run `koh list` for an interactive picker: navigate with the arrow keys or `j`/`k`, press `enter` to switch to the highlighted worktree, and `q` to quit.

### Normal development workflow

Once your session is set up:

1. Work in your configured environment (editor, dev server, etc.)
2. Make commits as normal
3. Push your branch and create a PR when ready

### Cleaning up after you're done

When your work is merged and you want to clean up:

```bash
koh cleanup feature-auth
```

This will:

- Remove the git worktree (forced — see warning below)
- Close the tmux window with all its panes

> **⚠️ Warning:** `koh cleanup` force-removes the worktree and **discards uncommitted changes without prompting**. Commit, stash, or push anything you want to keep before running it. If you only want to remove worktrees that are safe to delete, use `koh prune` — it skips worktrees with uncommitted changes.

### Pruning many worktrees at once

After a long-running project, `.koh/` accumulates worktrees whose work has already been merged. `koh prune` clears them out without you naming each one:

```bash
koh prune              # interactive picker (prunable worktrees pre-checked)
koh prune --dry-run    # preview what would be pruned, change nothing
koh prune --yes        # skip the picker, prune everything classified as prunable
```

Useful flags:

- `--delete-branch` — also delete the local branch for each pruned worktree
- `--no-fetch` — skip the implicit `git fetch --prune` (use offline)

In the interactive picker:

- `space` — toggle the highlighted worktree
- `a` / `n` — select all / none
- `d` — toggle "also delete local branch" for this run
- `enter` — prune the selected worktrees
- `q` — cancel without changes

## Commands

```bash
koh new <worktree-name>      # Create a new worktree and tmux session
koh switch <worktree-name>   # Switch to an existing worktree's tmux window
koh cleanup <worktree-name>  # Remove worktree (forced) and close tmux window
koh prune                    # Bulk-remove merged or stale worktrees
koh list                     # List all koh worktrees (interactive picker)
koh init                     # Interactive configuration setup
koh config                   # View current configuration
koh version                  # Display koh version
koh help                     # Show help message
```

## How it works

`koh` creates a new git worktree in the `.koh/` directory and opens a tmux window with panes configured based on your `.kohconfig` file. The first pane runs your setup script, and additional panes run any commands you've configured (dev server, editor, etc.).

The cleanup command force-removes the git worktree, then finds the tmux window by name and closes it. **⚠️ Uncommitted changes in the worktree are discarded without prompting** — commit, stash, or push anything you want to keep before running cleanup. For bulk cleanup that only touches worktrees that are safe to delete, use `koh prune` instead: it skips worktrees with uncommitted changes.

## Worktree Management

All worktrees are created in a `.koh/` directory at the root of your repository. This keeps your repository organized and makes it easy to:

- See all active worktrees: `ls .koh/`
- Clean up a worktree: `koh cleanup <name>`
- Manually remove worktrees: `git worktree remove .koh/<name>`
- List all worktrees: `git worktree list`

You may want to add `.koh/` to your `.gitignore` file.

**Tip:** Use `koh cleanup` instead of manually removing worktrees - it will close the tmux window and clean up the worktree in one command!

## Configuration

Before creating your first worktree, run `koh init` to set up your configuration. The tool will prompt you for:

- Path to your setup script (if you have one)
- Additional commands to run in tmux panes

The configuration is stored in `.kohconfig` at your repository root and can be updated anytime with `koh init`.

## Contributing

Feel free to submit issues or pull requests!

## License

[MIT](LICENSE)
