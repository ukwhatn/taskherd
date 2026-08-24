taskherd — a local kanban task board for herdr, linking tasks to agent sessions, GitHub PRs/Issues, and Jira tickets. See below for setup (Japanese; UI text is Japanese).

# taskherd

herdr 内で並走する複数のコーディングエージェントセッションを、ローカルの kanban タスク管理で束ねるツール。タスクに herdr のエージェントセッション・GitHub の PR/Issue・Jira チケットを紐づけ、それぞれのライブ状態をボードで一望できる。

Go 製の単体 CLI/TUI であり、同時に herdr プラグインとしても動く。herdr が無くても add/list/move/note/link といったタスク管理の核はすべて動作し、herdr 連携（セッションのライブ状態・ジャンプ）は到達できたときだけ加算される。

## インストール

### herdr プラグインとして

```
herdr plugin install ukwhatn/taskherd
```

プラグインの `[[build]]` が `go build` を実行するため、**Go toolchain がインストール環境に必要**（`go` が `PATH` 上にあること）。ビルドが失敗すると install 全体が中止され、プラグインは登録されない。

ローカルで変更を試す場合は `plugin install` の代わりに `plugin link` を使う。`plugin link` は build を実行しないため、事前に自分でビルドしておく:

```
go build -o bin/taskherd ./cmd/taskherd
herdr plugin link /path/to/taskherd
```

### 単体 CLI として

herdr を使わない場合や、herdr 外から直接操作したい場合は通常の Go バイナリとして導入できる:

```
go install github.com/ukwhatn/taskherd/cmd/taskherd@latest
```

## keybinding の設定

herdr プラグインのマニフェスト（`herdr-plugin.toml`）は action だけを宣言し、キーへの割り当てはユーザーの `~/.config/herdr/config.toml` に自分で書く。以下を追記する:

```toml
[[keys.command]]
key = "prefix+t"
type = "plugin_action"
command = "taskherd.open-board"
description = "open task board"

[[keys.command]]
key = "prefix+shift+t"
type = "plugin_action"
command = "taskherd.link-pane"
description = "link pane to task"
```

- `taskherd.open-board`: kanban ボードをオーバーレイで開く
- `taskherd.link-pane`: 今いる pane をタスクに紐づける picker をポップアップで開く

`link-pane` の実体は 2 段構成になっている。action 自身は対象 pane を特定して `plugin pane open --entrypoint picker` を呼ぶだけで、実際の選択 UI は別プロセス（`taskherd picker`）が担う。これは popup 実体に `HERDR_PANE_ID` が注入されない herdr 側の制約に合わせた構成で、キー1つの体験としては変わらない。

## config.toml のセットアップ

```
taskherd config init
```

`~/.config/taskherd/config.toml`（`TASKHERD_CONFIG` で上書き可）に既定設定を生成する。GitHub Enterprise Server や Jira を使う場合はコメントアウトされた項目を書き換える:

```toml
[github]
ghes_hosts = ["github.example.com"]

[jira]
site = "your-tenant.atlassian.net"
email = "you@example.com"
token_env = "TASKHERD_JIRA_TOKEN"
```

`site` / `email` は自分のテナントの値に置き換える。`ghes_hosts` は GitHub.com 以外の PR/Issue リンクを `github_pr` / `github_issue` として判別させたいホストの一覧。

### note を開くエディタ

note の編集に使うエディタは、次の順で解決する:

1. config.toml トップレベルの `editor`
2. `$VISUAL`
3. `$EDITOR`

```toml
editor = "nano"
```

herdr プラグインが開いた pane は shell を経由しないため `$EDITOR` が届かないことがある。board から note を編集するなら `editor` を書いておくのが確実。値は空白区切りで引数を書ける（例: `editor = "code -w"`）。

### Jira トークンの設定

Jira のトークンは config.toml に書かず、`token_env` が指す環境変数（既定 `TASKHERD_JIRA_TOKEN`）から読む:

```
export TASKHERD_JIRA_TOKEN="..."
```

トークンは https://id.atlassian.com/manage-profile/security/api-tokens で発行する。Atlassian は発行したトークンを 1 年で失効させるため、401 が返るようになったら再発行して環境変数を更新する。

### herdr integration の更新

セッションバッジの精度（herdr が Claude Code のエージェント状態をどこまで細かく検出できるか）は herdr 側の統合フックのバージョンに依存する。古い統合のままだと `agent_status` の精度が落ちるため、次のコマンドで最新化しておくことを推奨する:

```
herdr integration install claude
```

## 主要コマンド早見表

| コマンド | 内容 |
|---|---|
| `taskherd add <title> [--status S] [--due D] [--note N] [--link URL]... [--session current\|<uuid>]` | タスクを作成する |
| `taskherd list [--status S]... [--all] [--json]` | 一覧表示（既定は完了・却下列を除く） |
| `taskherd show <id>` | 詳細（note・リンクのライブ状態・紐づくセッション） |
| `taskherd edit <id> [--title] [--due] [--status]` | 属性を更新する |
| `taskherd note <id> [--set TEXT\|--append TEXT]` | note を編集する（既定は config の `editor` / `$VISUAL` / `$EDITOR`） |
| `taskherd move <id> <status>` / `taskherd done <id>` | 列を移動する |
| `taskherd link <id> <url> [--note N]` / `taskherd unlink <id> <url>` | 外部リンクの付け外し |
| `taskherd session link <id> [--current\|--session-id UUID\|--pane PANE_ID]` | エージェントセッションを紐づける |
| `taskherd jump <id> [--session UUID]` | 紐づいたセッションへ移動する（消滅していれば resume 起動） |
| `taskherd refresh [<id>] [--all]` | リンクのライブ状態を即時取得する |
| `taskherd board` | kanban ボード（TUI）を開く |
| `taskherd rm <id> [--yes]` | タスクを削除する |
| `taskherd config path` / `taskherd config init` | パス確認・既定 config.toml の生成 |

各コマンドは `--json` を付けると非対話・機械可読な出力になる（対話が必要な状況ではエラー終了し、`--yes` 等の代替フラグを案内する）。

## board の主なキー操作

`taskherd board` を起動すると kanban ボードが開く。操作は GUI と同じ文法で統一してある: **移動＝矢印 / 決定＝Enter / 切替＝Tab / 取消＝Esc / 削除＝Delete**。覚えるべき文字キーは 6 つだけ。

| キー | 動作 |
|---|---|
| `←` `→` | 列フォーカスの移動 |
| `↑` `↓` | カードの移動 |
| `Tab` | 移行先ステータスのセレクタを開く（次の列が既定選択。`←→` で選び `Enter` で確定、`Esc` で取消） |
| `Enter` | 詳細モーダル |
| `Delete` / `Backspace` | タスク削除（`y`/`n` の確認あり） |
| `a` | タスク追加モーダル |
| `g` | 紐づくセッションへジャンプ（複数あれば `↑↓` で選択） |
| `r` / `R` | フォーカスカード / 全体のライブ再取得 |
| `t` | 完了・却下列（terminal 列）の折り畳み切替 |
| `q` / `Ctrl+C` | 終了 |

### 画面の見え方

- カードは角丸ボーダーのボックスで描画し、選択中のカードは列の色でボーダーを強調する。列ヘッダは列色のラベルと件数、フォーカス中の列は反転表示
- 列に入りきらないカードは上下の `↑ N件` / `↓ N件` で件数を示し、カーソルの移動に追従してスクロールする（黙って切り落とさない）
- 詳細・追加・セレクタ・確認ダイアログは、タイトル付きのボーダーボックスとして画面中央に重ねて描画する。背後のボードはそのまま見える
- 端末が狭いときは、列間の余白 → カードのボーダー → 表示する列数（横スクロール）の順に削る。配色は ANSI 16 色のみを使うため、端末のテーマにそのまま追従する

### 詳細モーダル（`Enter`）

項目を `↑↓` で選び `Enter` で編集する、の 1 文法だけで動く。項目は タイトル / ステータス / 期限 / note / 各リンク / ＋リンクを追加 / 各セッション / ＋セッションを紐づける。

- **ステータス**: 選択中に `←→` でその場切替（`Enter` ならセレクタを開く）
- **note**: `Enter` でエディタを開く（config の `editor` / `$VISUAL` / `$EDITOR`。board は一時的に退避する）
- **リンク行**: `Enter` でリンクメモの編集、`Delete` で確認付きの解除
- **＋リンクを追加**: 空白・改行区切りで複数 URL を一括登録できる（1 つでも不正なら全体を中止する）
- **セッション行**: `Enter` でそのセッションへジャンプ、`Delete` で確認付きの解除
- **＋セッションを紐づける**: herdr が検出しているエージェント一覧から `↑↓` で選ぶ。herdr 不達時は無効表示になる
- `Esc` で board へ戻る

### 追加モーダル（`a`）

詳細モーダルと同じ項目リスト（タイトル / ステータス / 期限 / note / リンク）だが、**フォーカス中の項目はそのまま入力できる**（`Enter` で開く操作は不要）。

- `↑↓` で項目移動。移動しても入力内容は保持される
- ステータスは現在フォーカスしている列が既定。`←→` で切替
- **`Enter` はどの項目にいても「作成して閉じる」**（タイトルが空ならエラーを出して閉じない）。`Esc` で取消
- タイトルに複数行を貼り付けると **1 行＝1 タスク**として一括作成する（他の項目の値は全行に適用される）
- `Enter` は作成に使うため、**改行は `Shift+Enter`**（拡張キーボードプロトコル対応端末）**または `Ctrl+J`** で入れる。タイトルの改行は 1 行＝1 タスク、note の改行はそのまま複数行 note になる。どちらのキーが有効かはフッタのキーヘルプに表示される

### 入力欄について

- **ペースト**: ブラケットペーストに対応しているので、URL やタスク一覧はターミナルからそのまま貼り付けられる
- **日本語入力**: IME の確定文字列はコマンドキーと解釈せず常に入力欄へ入る（確定文字列が飲み込まれない）。入力欄を持たない画面（確認ダイアログ・セレクタ）でも、複数文字がまとめて確定したイベントはキー名として解釈しない
