# taskherd

A kanban board for the work your coding agents are doing.

Tasks live on your machine as one JSON file. Each one can carry agent sessions, GitHub pull
requests and issues, and Jira tickets — and the board shows what all of them are doing right now,
side by side.

[日本語版 README](README.ja.md)

![The taskherd board](docs/assets/board-en.png)

## What it gives you

- **One board for parallel agents.** When several agent sessions run at once, the thing that gets
  lost is which one is on what. A task holds its sessions, so the board answers that in a glance —
  and `g` jumps to the session, or starts one if there is none yet.
- **Live state, not stale links.** A pull request's CI, its review decision, and a Jira ticket's
  status are fetched and cached, so a card shows `CI✓ rv✓` rather than a URL you have to open.
- **Works without any of it.** No herdr, no `gh`, no network: add, list, move, note and link all
  keep working. The integrations add to the board; nothing depends on them.
- **A CLI that scripts.** Every command takes `--json` and never prompts under it.
- **Japanese or English.** `language = "ja"` or `"en"`, or `TASKHERD_LANG` for one invocation.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/ukwhatn/taskherd/main/install.sh | sh
```

That drops a binary in `~/.local/bin`, which `taskherd update` can replace later without sudo.
Alternatives: download an archive from [releases](https://github.com/ukwhatn/taskherd/releases), or
`go install github.com/ukwhatn/taskherd/cmd/taskherd@latest`.

As a [herdr](https://herdr.dev) plugin:

```sh
herdr plugin install ukwhatn/taskherd
```

Requires macOS or Linux. The default icons need a Nerd Font; `board.icons = "ascii"` if you would
rather not.

## Quick start

```sh
taskherd config init                                   # write a commented config.toml
taskherd add "Rate limit the search endpoint" --due 2026-09-02
taskherd link 1 https://github.com/acme/webapp/pull/482
taskherd start 1 --cwd ~/src/webapp                    # open an agent session on it
taskherd board                                         # the board
```

On the board: arrows move, `Enter` opens a task, `Tab` changes its column, `a` adds one, `g` goes
to its session, `q` quits.

![A task's detail](docs/assets/detail-en.png)

## Documentation

| | |
|---|---|
| [Commands](docs/commands.md) | Every command and flag |
| [Configuration](docs/configuration.md) | Every setting in `config.toml`, and the environment it reads |
| [Keybindings](docs/keybindings.md) | The board's keys, mouse, and how to read a card |
| [herdr integration](docs/herdr-integration.md) | Installing as a plugin, binding keys, starting sessions |
| [Development](docs/development.md) | Building, testing, and how the packages fit together |

## License

MIT — see [LICENSE](LICENSE).
