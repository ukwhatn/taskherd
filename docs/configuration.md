# Configuration

`taskherd config init` writes a commented `config.toml` to `~/.config/taskherd/config.toml`, or
wherever `TASKHERD_CONFIG` points. `taskherd config path` prints where everything lives.

The file is written in the language taskherd is running in, so `TASKHERD_LANG=en taskherd config
init` gives you an English one.

```toml
language = "en"      # "ja" (default) or "en"
editor = "nano"      # optional; falls back to $VISUAL, then $EDITOR

[board]
refresh_interval_minutes = 10
cache_ttl_minutes = 5
icons = "nerd"       # "nerd" (default) / "ascii" / "none"
hyperlinks = true

[update]
check = true

[github]
ghes_hosts = ["github.example.com"]

[github.accounts]
"github.com/some-org" = "work-account"

[jira]
site = "your-tenant.atlassian.net"
email = "you@example.com"
token_env = "TASKHERD_JIRA_TOKEN"
```

## Language

`language` picks the language of the board, the CLI's help, and its output. `TASKHERD_LANG`
overrides it for one invocation:

```sh
TASKHERD_LANG=en taskherd board
```

There is no detection from `LANG`. A shell's `LANG` frequently disagrees with the language someone
actually reads, and a board that silently changes language is worse than one that stays put.

Set it in `config.toml` rather than the environment if you use taskherd as a herdr plugin: the
board is spawned by the long-running herdr server and inherits *its* environment, so a variable
exported in your shell may never arrive. The same applies to `jira.token_file` below.

## Board

### `refresh_interval_minutes`, `cache_ttl_minutes`

How often the board fetches live state in the background, and how long a fetched value counts as
current. `refresh_interval_minutes = 0` turns the background fetch off entirely; `r` and `R` still
fetch on demand.

### `icons`

Which glyphs the cards, session badges, overflow indicators and key help draw with.

| Value | What you get |
|---|---|
| `nerd` (default) | Nerd Font glyphs. Your terminal font has to be one (JetBrainsMono Nerd Font, Hack Nerd Font, …) |
| `ascii` | `PR` `IS` `JR` `+` `!` `*` and friends |
| `none` | No symbols at all — states are spelled out (`open`, `working`, `pass`) |

No mode uses East Asian Ambiguous-width characters, or symbols a Japanese font draws full-width
(`✓` `●` `↑` `…`). A terminal reserves one cell for those while the font draws two, which pulls
every card border out of line.

### `hyperlinks`

With `true` (the default) link rows are wrapped in OSC 8, so a terminal that understands it
(ghostty, iTerm2, WezTerm) opens them on a click. A terminal that does not shows plain text — the
escapes are not made visible — so the only reason to turn this off is a relay in between that does
not pass OSC 8 through.

## Updates

```toml
[update]
check = true
```

With `check = true` the board asks GitHub, at most once a day, whether a newer release exists, and
says so on the status line. Nothing else contacts the network on its own: short-lived commands read
what the board recorded and never ask themselves.

`check = false` stops it entirely — no checker is built, so there is nothing to ask.
`TASKHERD_NO_UPDATE_CHECK=1` does the same for one invocation. `taskherd update` keeps working
either way, since that is someone asking on purpose.

The only thing sent is the HTTP request itself, to
`api.github.com/repos/ukwhatn/taskherd/releases/latest`.

## GitHub

`ghes_hosts` lists the GitHub Enterprise Server hosts whose pull request and issue URLs should be
recognised as such. Without it, a link to a non-github.com host is filed as `other` and no live
state is fetched for it.

### `[github.accounts]`

By default a fetch runs as whatever account `gh` currently has active. Naming an account here makes
taskherd resolve its token with `gh auth token --hostname <host> --user <account>` and hand that to
the `gh` subprocess — **without switching your active account**.

A key is either `"<host>"` or `"<host>/<owner>"`, resolved owner-first, then host, then `gh`'s
active account. Case and surrounding whitespace are ignored.

```toml
[github.accounts]
"github.com/some-org" = "work-account"     # the org's repositories, as work
"github.com/your-account" = "personal"     # your own, as you
"github.example.com" = "enterprise"        # a whole host
```

One host commonly carries both personal and organization repositories, and **a host-level entry
cannot serve both**. Asking for a repository the authenticated account cannot see gets
`Could not resolve to a Repository` from GitHub's GraphQL API — which is indistinguishable from a
repository that does not exist — so every link on that host fails. An owner-level entry lets one
refresh cycle use both accounts.

- The token exists only in memory: never in the config, `cache.json`, a log, or an error message.
- Resolution is cached per host and account, so several owners sharing an account cost one
  `gh auth token` call.
- If a named account's token cannot be resolved, the fetch falls back to the active account, and
  says so only if that fetch then fails as well.
- A `Could not resolve to a Repository` failure reports **which account it ran as** and points at
  the owner form.

## Jira

The token is never written in `config.toml`. It is read from the environment variable `token_env`
names (default `TASKHERD_JIRA_TOKEN`), and failing that from the file `token_file` names:

```sh
export TASKHERD_JIRA_TOKEN="..."
```

**Use `token_file` if you open the board as a herdr plugin.** That pane is started by the herdr
server, so a variable exported in a shell never reaches it. A file is read the same way however the
board was started:

```sh
umask 077 && printf '%s' "$TOKEN" > ~/.config/taskherd/jira_token
```

```toml
[jira]
token_file = "~/.config/taskherd/jira_token"
```

A leading `~/` expands against `HOME`. Put the token in the file and nothing else; surrounding
whitespace and the trailing newline are stripped on read. Keep it at mode `600`.

When the file cannot be read the reason shows up on the board as the link's fetch failure. The
token itself reaches neither the error nor the cache.

Issue tokens at https://id.atlassian.com/manage-profile/security/api-tokens. Atlassian expires them
a year after they are issued, so reissue when 401s start.

## Editor for notes

Resolved in this order:

1. `editor` at the top level of `config.toml`
2. `$VISUAL`
3. `$EDITOR`

```toml
editor = "code -w"
```

Arguments can be included, separated by spaces. A pane opened by the herdr plugin bypasses your
shell, so `$EDITOR` may not arrive — set `editor` if you edit notes from the board.

## Columns

```toml
[[columns]]
id = "todo"
label = "ToDo"
kind = "open"       # "open" or "terminal"
color = "gray"
```

The order of the blocks is the order on the board. A `terminal` column is folded on the board by
default (`t` unfolds) and left out of `list` unless `--all` is given. Colors are ANSI 16 names, so
they follow your terminal's theme.

Tasks whose status matches no column still show, in an `(unknown)` column: a column removed from
the config does not lose the tasks that were in it.

## Session start

See [herdr integration](herdr-integration.md#the-prompt-a-session-starts-with) for
`[session_start]`.

## Paths

| | |
|---|---|
| `config.toml` | `$TASKHERD_CONFIG`, else `~/.config/taskherd/config.toml` |
| `tasks.json`, `cache.json`, `update.json` | `$XDG_STATE_HOME/taskherd/`, else `~/.local/state/taskherd/` |

`taskherd config path` prints all of them, including the backup and lock files.
