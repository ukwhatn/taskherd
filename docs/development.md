# Development

```sh
go build ./...
go test ./...          # never touches real user data
go test -race ./...
gofmt -l . && go vet ./...
```

To try local changes under herdr:

```sh
go build -o bin/taskherd ./cmd/taskherd
herdr plugin link .    # link does not build, so build first
```

Go 1.25 or newer.

## Packages

| | |
|---|---|
| `model` | The task model, its validation, and classifying a URL |
| `store` | tasks.json: flock, atomic writes, change watching |
| `atomicfile` | Replacing a file's contents in one step |
| `config` | Reading config.toml and resolving paths |
| `i18n` | Every string the UI can show, in both languages |
| `herdrc` | herdr: snapshots, event subscription, pane operations |
| `fetch` | Live state from `gh` and Jira, and cache.json |
| `update` | Checking for a newer release and replacing the binary |
| `buildinfo` | Which build is running |
| `tui` | The bubbletea v2 board, its modals, and the picker |
| `cli` | The cobra commands and `--json` output |

## Adding or changing UI text

Every string a user reads lives in `internal/i18n`, in both `ja.go`/`cli_ja.go`/`errors_ja.go` and
their `_en` counterparts. `TestCatalogsComplete` fails if either language leaves a field empty, and
`TestCatalogsAgreeOnFormatArguments` fails if the two disagree about what a format string takes —
both of which are invisible at runtime, which is why they are caught in a test.

An error type says itself in a language by implementing `i18n.Localizer` next to its own
definition; `i18n` depends on nothing else in the tree, and it stays that way. An error's own
`Error()` is English on purpose: it is what lands in a log, where one searchable wording beats a
familiar one. Diagnostics that only ever appear in a log — a failed rename, an unparsable body —
are written in English directly and never enter the catalog.

## Screenshots

`scripts/demo/record.sh` regenerates `docs/assets`. It needs [vhs](https://github.com/charmbracelet/vhs)
(`brew install vhs`) and a Nerd Font.

The fixtures in `scripts/demo` are invented — `acme/webapp`, `acme.atlassian.net` — and the demo
config disables both the background fetch and the update check, so a recording never reaches the
network and no real task, repository or ticket can end up in a published image. Keep it that way.

## Releasing

A tag starting with `v` triggers `.github/workflows/release.yml`, which runs the tests and then
GoReleaser. Check the config before tagging:

```sh
goreleaser check
goreleaser release --snapshot --clean
```

The snapshot lands in `dist/`. Unpack one and run `taskherd version` to confirm the build stamped
itself.

Archive names deliberately leave the version out (`taskherd_darwin_arm64.tar.gz`), because
`install.sh` and `taskherd update` both build that URL from a tag they already hold, and a name that
repeats the tag would make it a parse instead of a concatenation.
