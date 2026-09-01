# Commands

Every command takes `--json`, which makes it non-interactive and machine-readable. Under `--json` a
command that would have prompted fails instead, and says which flag (`--yes`, and so on) replaces
the prompt.

## Tasks

| | |
|---|---|
| `taskherd add <title> [--status S] [--due D] [--note N] [--link URL]... [--session current\|<uuid>]` | Create a task |
| `taskherd list [--status S]... [--all] [--json]` | List tasks; terminal columns are left out unless `--all` |
| `taskherd show <id>` | Everything about one task: note, links with their live state, sessions |
| `taskherd edit <id> [--title] [--due] [--status]` | Change its attributes |
| `taskherd note <id> [--set TEXT\|--append TEXT]` | Edit the note; with neither flag, in `editor` / `$VISUAL` / `$EDITOR` |
| `taskherd move <id> <status>` | Move it to another column |
| `taskherd done <id>` | Move it to the first terminal column |
| `taskherd rm <id> [--yes]` | Delete it |

## Links

| | |
|---|---|
| `taskherd link <id> <url> [--note N]` | Attach a pull request, issue or ticket |
| `taskherd unlink <id> <url>` | Detach one |
| `taskherd refresh [<id>] [--all]` | Fetch live state now, rather than waiting for the board |

## Sessions

| | |
|---|---|
| `taskherd session link <id> [--current\|--session-id UUID\|--pane PANE_ID]` | Attach an agent session |
| `taskherd session unlink <id> <uuid>` | Detach one |
| `taskherd jump <id> [--session UUID] [--space ID\|--new-space LABEL]` | Go to the attached session, resuming it if its pane is gone |
| `taskherd start <id> [--cwd PATH] [--prompt TEXT] [--new] [--no-focus] [--space ID\|--new-space LABEL]` | Start a session on the task and attach it |

`start` needs `--cwd` when no candidate can be derived from existing sessions. It reuses the pane it
started last time when it can — see
[herdr integration](herdr-integration.md#reusing-the-pane-from-last-time) — and `--new` skips that.
It focuses the new pane unless `--no-focus`.

**Which space it lands in.** Without `--space` or `--new-space` the pane goes wherever herdr would
put it, which is the focused space. `--space <workspace_id>` names an existing one; `--new-space
<label>` makes one and starts there, with an empty label leaving the naming to herdr. The two
cannot be combined. `herdr workspace list` prints the ids. A `start` that would recover last time's
pane refuses when that pane sits in a different space from the one named, the same way it refuses a
different `--cwd`. On `jump` the flags only apply to a resume, since a live pane is focused where it
already is.

## Everything else

| | |
|---|---|
| `taskherd board` | Open the kanban board |
| `taskherd config path` | Print where the config and data files are |
| `taskherd config init` | Write a commented config.toml, in the current language |
| `taskherd version` | Print the running build |
| `taskherd update [--check] [-y]` | Check for a newer release and install it |

`taskherd update` replaces the running binary in place. It refuses on a build from source, since
there is no release to compare it against. See [Configuration](configuration.md#updates) for how
the check authenticates itself, and for turning the background one off.
