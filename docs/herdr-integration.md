# herdr integration

taskherd works on its own. Everything here is what it gains from [herdr](https://herdr.dev) being
there: session state on the cards, jumping to a session, and starting one from a task.

Requires herdr 0.8.0 or newer.

## Installing as a plugin

```sh
herdr plugin install ukwhatn/taskherd
```

The manifest's `[[build]]` runs `go build`, so **a Go toolchain has to be on the installing
machine**. A failed build aborts the install and registers nothing.

To try local changes, use `plugin link` instead. It does not build, so build first:

```sh
go build -o bin/taskherd ./cmd/taskherd
herdr plugin link /path/to/taskherd
```

## Binding keys

The manifest (`herdr-plugin.toml`) declares actions only; which key runs them is yours to write in
`~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+space"
type = "plugin_action"
command = "taskherd.open-board"
description = "open task board"

[[keys.command]]
key = "prefix+t"
type = "plugin_action"
command = "taskherd.link-pane"
description = "link pane to task"
```

- `taskherd.open-board` opens the kanban board as an overlay.
- `taskherd.link-pane` opens, in a popup, either the current pane's task — if its session is already
  attached to one — or a picker to attach it to a task.

`link-pane` is two-stage internally: the action locates the target pane and calls
`plugin pane open --entrypoint picker`, and a separate process (`taskherd picker`) decides what to
show. That shape exists because herdr does not inject `HERDR_PANE_ID` into the popup itself; as one
keystroke it makes no difference.

`taskherd picker` starts by asking whether the agent herdr sees in the target pane has a session
attached to any task. If it has, the picker is skipped and that task's detail opens directly, with
`Esc` closing the popup outright rather than falling back to the board. If the same session is
attached to several tasks, the lowest id wins. If herdr is unreachable, no agent is found in the
pane, there is no session id, or nothing matches — the picker opens as usual.

## The prompt a session starts with

Pressing `g` on a task with no session, or running `taskherd start`, opens an agent in a new pane
and sends it `prompt_template`, expanded:

```toml
[session_start]
prompt_template = """
Please work on #{{id}} {{title}}.

Current status: {{status}}

{{note}}
{{links}}"""
```

The placeholders are `{{id}}`, `{{title}}`, `{{note}}`, `{{status}}` and `{{links}}`. `{{links}}`
expands to one `- <url>` per link, and its whole line disappears when there are none.

Columns that need a different opening can override it per column id. A column not named here uses
`prompt_template`:

```toml
[session_start.templates]
"review" = "Go over #{{id}} {{title}} and tell me what to look for.\n\n{{links}}"
"todo" = ""
```

Quote the column ids — an id containing a dot would otherwise be read as a TOML dotted key. `""`
starts the agent without sending anything; setting `prompt_template` itself to `""` does that for
every column that has no override.

## Reusing the pane from last time

`taskherd start` and the board's `g` reuse the agent they started for this task last time — named
`taskherd-<id>` — if it is still on herdr, rather than making another pane:

| Last time's agent | What happens |
|---|---|
| Idle, session known, unattached, same cwd | That pane is attached and the prompt is sent (`reused: true` under `--json`) |
| Already attached to this task | Nothing starts; you are pointed at `jump` |
| Blocked, or no session id yet | Nothing starts; you are pointed at the pane |
| A different cwd was asked for | Neither reused nor started — you are pointed at the existing pane or `--new`, rather than having the cwd you asked for quietly ignored |
| Not found | A new one starts, as usual |

`--new` skips the reuse check when a second session is what you actually want. Only the herdr agent
name gets a suffix (`taskherd-43-2`, the lowest free number).

Starting, attaching and jumping all set `#<id> <title>` on the agent's row in herdr's sidebar
(`pane report-metadata --display-agent`). That is a display name only, distinct from the agent name
used as a target by `agent focus`, so `--new`'s numbering never shows up in it.

## A launch from the board runs outside it

The board's `g` does not perform the launch. It spawns `taskherd start <id> --cwd ... --prompt ...`
**as a process detached from the board, and closes immediately.** A resume — restarting a session
whose pane is gone — goes the same way, to `taskherd jump <id> --session <uuid> --yes`.

The board is a herdr overlay pane, and it has to close for you to see the tab that was started.
Meanwhile creating the pane, attaching the session and sending the prompt takes **around thirty
seconds**, between waiting for the agent to accept input and waiting for Claude's integration hook
to report a session id. Run inside the board, all of that is lost the moment the board closes,
leaving a pane and an agent behind and nothing attached. Hence the detachment.

- **You land on the new pane immediately** (`tab create --focus`), without waiting for the agent to
  come up or the session id to appear — about a second after the board closes. A reused pane is
  reached with `agent focus`.
- The launch's output is appended to `$XDG_STATE_HOME/taskherd/detached.log` (by default
  `~/.local/state/taskherd/detached.log`).
- A failure arrives as a herdr notification with the reason. The pane stays, so you can catch up by
  hand from **attach session** in the detail modal, or press `g` again to reuse it.
- Jumping to a pane that is alive is the one thing the board does itself, since it is a single
  `agent focus`. The board closes on success.

`taskherd start` from a shell also focuses by default. `--no-focus` starts it in the background
instead.

## Keeping the integration current

How precisely herdr can tell what a Claude Code agent is doing depends on the version of its
integration hook. An old one degrades `agent_status`, and with it the session badges:

```sh
herdr integration install claude
```
