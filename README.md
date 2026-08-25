taskherd — a local kanban task board for herdr, linking tasks to agent sessions, GitHub PRs/Issues, and Jira tickets. See below for setup (Japanese; UI text is Japanese).

# taskherd

herdr 内で並走する複数のコーディングエージェントセッションを、ローカルの kanban タスク管理で束ねるツール。タスクに herdr のエージェントセッション・GitHub の PR/Issue・Jira チケットを紐づけ、それぞれのライブ状態をボードで一望できる。

Go 製の単体 CLI/TUI であり、同時に herdr プラグインとしても動く。herdr が無くても add/list/move/note/link といったタスク管理の核はすべて動作し、herdr 連携（セッションのライブ状態・ジャンプ）は到達できたときだけ加算される。

## 動作要件

- macOS / Linux
- [herdr](https://herdr.dev) 0.8.0 以上（herdr 連携機能を使う場合。単体 CLI としては不要）
- Go 1.25 以上（ビルド時。`herdr plugin install` はインストール先の環境でビルドする）
- Nerd Font 対応の等幅フォント（既定のアイコン表記に必要。`board.icons = "ascii"` にすれば不要）
- GitHub のライブ状態取得には [gh CLI](https://cli.github.com/) の認証が必要（Jira は API トークン）

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

- `taskherd.open-board`: kanban ボードをオーバーレイで開く
- `taskherd.link-pane`: 今いる pane が既にタスクへ紐づいていればそのタスクの詳細モーダルを、まだ紐づいていなければタスクに紐づける picker をポップアップで開く

`link-pane` の実体は 2 段構成になっている。action 自身は対象 pane を特定して `plugin pane open --entrypoint picker` を呼ぶだけで、実際の判定・UI は別プロセス（`taskherd picker`）が担う。これは popup 実体に `HERDR_PANE_ID` が注入されない herdr 側の制約に合わせた構成で、キー1つの体験としては変わらない。

`taskherd picker` は起動時に、対象 pane に herdr が検出しているエージェントのセッションが、tasks.json 上のいずれかのタスクに紐づいているかを調べる。紐づいていれば picker を開かずそのタスクの詳細モーダルを直接開き、`Esc` で（board へ戻らず）ポップアップごと終了する。同じセッションが複数のタスクに紐づいている場合は id が最小のタスクを開く。herdr に到達できない・pane からエージェントを検出できない・セッション id が無い・どのタスクにも紐づいていない、のいずれの場合も判定を諦めて従来どおり picker を開く。

## config.toml のセットアップ

```
taskherd config init
```

`~/.config/taskherd/config.toml`（`TASKHERD_CONFIG` で上書き可）に既定設定を生成する。GitHub Enterprise Server や Jira を使う場合はコメントアウトされた項目を書き換える:

```toml
[board]
icons = "nerd"      # nerd（既定・Nerd Font 必須）/ ascii / none
hyperlinks = true   # リンク行を OSC 8 でクリック可能にする

[github]
ghes_hosts = ["github.example.com"]

[github.accounts]
"github.com/your-account" = "your-account"
"github.com/some-org" = "work-account"
"github.example.com" = "your-enterprise-account"

[jira]
site = "your-tenant.atlassian.net"
email = "you@example.com"
token_env = "TASKHERD_JIRA_TOKEN"
```

`site` / `email` は自分のテナントの値に置き換える。`ghes_hosts` は GitHub.com 以外の PR/Issue リンクを `github_pr` / `github_issue` として判別させたいホストの一覧。

### アイコン表記（`board.icons`）

カードのリンク行・セッション状態・オーバーフローインジケータ・キーヘルプで使う記号を選ぶ。

| 値 | 内容 |
|---|---|
| `nerd`（既定） | Nerd Font のグリフを使う。端末フォントに Nerd Font（JetBrainsMono Nerd Font など）が必要 |
| `ascii` | `PR` `IS` `JR` `+` `!` `*` などの ASCII 記号で代替する |
| `none` | 記号を使わず、状態を単語（`open` / `working` / `pass`）で書く |

いずれのモードでも、**東アジア曖昧幅（EAW=Ambiguous）の文字と、和文フォントが全角で描く記号（`✓` `●` `↑` `…` など）は使わない**。これらは端末が 1 セルを確保するのに和文フォントが 2 セル分の字形を描くため、日本語フォントを併用した端末でカードの罫線がずれる。

### リンクのクリック（`board.hyperlinks`）

`true`（既定）のとき、カードと詳細モーダルのリンク行を OSC 8 でハイパーリンクにする。対応端末（ghostty / iTerm2 / WezTerm など）ではクリックで既定ブラウザが開く。非対応端末では通常のテキストとして表示される（エスケープは可視化されない）ので、無効化が必要なのは OSC 8 を素通ししない中継を挟む場合だけ。

### gh のアカウント指定（`[github.accounts]`）

複数アカウントで gh にログインしている場合、ライブ取得は既定では gh の active account に従う。`[github.accounts]` にアカウント名を書くと、`gh auth token --hostname <host> --user <account>` で取得したトークンを gh サブプロセスの環境変数として渡し、**active account を切り替えずに**そのアカウントで取得する。

キーは `"<host>"` と `"<host>/<owner>"` の 2 形式を書ける。解決順は **owner 完全一致 → ホスト → gh の active account**（大文字小文字と前後の空白は無視）。

```toml
[github.accounts]
"github.com/some-org" = "work-account"   # 組織のリポジトリは業務アカウントで
"github.com/your-account" = "personal"   # 個人のリポジトリは個人アカウントで
"github.example.com" = "enterprise"      # ホスト単位（従来の形式）
```

同一ホストに個人リポジトリと組織リポジトリが混在する場合、**ホスト単位の指定では解決できない**。片方のアカウントから見えないリポジトリを引くと GitHub の GraphQL は `Could not resolve to a Repository` を返すため、そのホストのリンクが全件取得失敗になる。owner 単位で指定すると、1 回の取得サイクルで両方のアカウントを使い分ける。

- トークンは実行中のメモリにしか置かない（config・cache.json・ログ・エラーメッセージのいずれにも書かない）
- トークンの解決は「ホスト + アカウント名」単位でキャッシュするので、同じアカウントを共有する複数 owner でも `gh auth token` は 1 回しか走らない
- 指定したアカウントのトークンが取れない場合は active account にフォールバックし、その取得も失敗したときだけエラーにその旨を併記する
- `Could not resolve to a Repository` で失敗したときは、**どのアカウントで引いたか**と owner 単位指定の案内をエラーに併記する（トークンは出さない）
- 一致するキーがないリンクは従来どおり gh に任せる

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

Jira のトークンは config.toml に書かない。読む順序は `token_env` が指す環境変数（既定 `TASKHERD_JIRA_TOKEN`）、次に `token_file` が指すファイル:

```
export TASKHERD_JIRA_TOKEN="..."
```

**board を herdr プラグイン（`prefix+space`）から開くなら `token_file` を使う。** プラグインの pane は常駐している herdr サーバが起動するため、シェルで export した環境変数は届かない。ファイルなら board をどう起動しても同じように読める:

```
umask 077 && printf '%s' "$TOKEN" > ~/.config/taskherd/jira_token
```

```toml
[jira]
token_file = "~/.config/taskherd/jira_token"
```

先頭の `~/` は `HOME` に展開する。ファイルの中身はトークンだけを置き、前後の空白と改行は読み込み時に落とされる。パーミッションは `600` にしておく。

ファイルが読めない・空のときは、その理由がリンクの取得失敗としてボードに出る（`Jira の設定がない: token_file "..." を読めない: ...`）。トークン自体はエラーにもキャッシュにも書かれない。

トークンは https://id.atlassian.com/manage-profile/security/api-tokens で発行する。Atlassian は発行したトークンを 1 年で失効させるため、401 が返るようになったら再発行する。

### タスクからセッションを起こすプロンプト（`[session_start]`）

タスクにまだセッションが紐づいていない状態で `board` の `g` を押す・`taskherd start` を実行すると、新しい pane で agent を起動し、初期プロンプトとして `prompt_template` を展開した文面を送る:

```toml
[session_start]
prompt_template = """
#{{id}} {{title}} に取り組んでほしい。

現在のステータス: {{status}}

{{note}}
{{links}}"""
```

展開できるプレースホルダは `{{id}}` `{{title}}` `{{note}}` `{{status}}` `{{links}}`。`{{links}}` はリンクごとに `- <url>` の行になり、リンクが 1 つもなければその行ごと消える。

列によって始め方が違う場合は、列 id ごとに上書きできる。`[session_start.templates]` に無い列は `prompt_template` を使う:

```toml
[session_start.templates]
"review" = "#{{id}} {{title}} のレビュー観点を確認してほしい。\n\n{{links}}"
"todo" = ""
```

列 id はキーを必ず引用符付きで書く（ドットを含む id が TOML の dotted key として誤解釈されるのを防ぐため）。`""` を指定すると、その列からの起動はプロンプトを送らず起動だけする。`prompt_template` 自体を `""` にすれば、列の上書きが無いすべての起動でプロンプトを送らなくなる。

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
| `taskherd start <id> [--cwd PATH] [--prompt TEXT]` | 新しいエージェントセッションを起こし、紐づける（cwd 候補が定まらなければ `--cwd` が必須） |
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
| `g` | 紐づくセッションへジャンプ（複数あれば `↑↓` で選択）。紐づいていなければ起動モーダルを開く |
| `r` / `R` | フォーカスカード / 全体のライブ再取得 |
| `t` | 完了・却下列（terminal 列）の折り畳み切替 |
| `q` / `Ctrl+C` | 終了 |

### マウス操作

board はマウスも受け付ける（herdr の pane 内からのマウス転送が前提）。

| 操作 | 動作 |
|---|---|
| カードを左クリック | そのカードへ選択を移し、詳細モーダルを開く（`Enter` と同じ） |
| ホイール上下 | フォーカス中の列でカードを送る（`↑↓` と同じ） |
| ホイール左右 | 列フォーカスを送る（`←→` と同じ） |

盤面以外の場所のクリック・ホイールは無視する。詳細・追加・各種セレクタ・確認ダイアログが開いている間はマウス操作全体を無視する（背後のカードは押せない）。

### 画面の見え方

- カードは角丸ボーダーのボックスで描画し、選択中のカードは列の色でボーダーを強調する。列ヘッダは列色のラベルと件数、フォーカス中の列は反転表示
- **リンクは 1 件 = 1 行**で `<アイコン> <repo>#<番号> <状態>` の形に描画する（例: `<PRアイコン> owner/repo#123 CI<pass> rv<pass>`）。列が狭いときは `owner/repo#123` → `repo#123` → `#123` の順に落とし、番号は最後まで残す。リンクが多いカードは 3 行まで描画し、残りは `他 N 件` にまとめる
- リンク行の repo 名と番号は URL から取り出すので、ライブ取得の前でも・オフラインでもカードは何のリンクかを示す。キャッシュ由来なのは行末の状態だけ。未取得は `未取得`（灰）になる
- **状態は色で表す**（下表）。TTL 超過（stale）は**経過時間の表示だけを dim にし、状態そのものの色は保つ**。取得が失敗し続けている間は、最終成功値を残したまま行末に警告アイコン + 経過時間を赤で添えるので、「値は出ているが更新できていない」状態が見分けられる
- カードのタイトルは列の幅に合わせて **2 行まで折り返す**（1 行で収まるものは 1 行のまま）。2 行にも入りきらない分は行末を `~` で切る
- 列に入りきらないカードは上下のオーバーフローインジケータ（`<上矢印> N件` / `<下矢印> N件`）で件数を示し、カーソルの移動に追従してスクロールする（黙って切り落とさない）
- 折り畳んだ terminal 列は、盤面の**右端にラベルと件数の縦スタック**としてまとめる。折り畳み中は `←→` のフォーカス対象から外れ、`t` で開けば通常の列に戻る
- 詳細・追加・セレクタ・確認ダイアログは、タイトル付きのボーダーボックスとして画面中央に重ねて描画する。背後のボードはそのまま見える
- **列の幅はカードが読める幅（内容幅 24 セル）から決め、列数を端末幅に従わせる**。入りきらない列は横スクロールで到達する（`~ 列 3-5 / 6` のように現在位置を出す）。列間の余白やカードのボーダーは、削ることで 1 列多く並べられるときに削る。1 列も入らない幅では盤面を出さずその旨を表示する。配色は ANSI 16 色のみを使うため、端末のテーマにそのまま追従する

| 対象 | 色 |
|---|---|
| PR state | open=緑 / draft=灰 / merged=紫 / closed=赤 |
| CI（checks） | pass=緑 / fail=赤 / pending=黄 / 未設定=表示しない |
| review decision | approved=緑 / changes requested=赤 / review required=黄 |
| Issue state | open=緑 / closed=紫 |
| Jira statusCategory | new=灰 / indeterminate=黄 / done=緑 |
| セッション状態 | blocked=赤 / working=緑 / done=黄 / idle=灰 / offline=灰 dim |
| 期限 | 超過=赤 / 当日・翌日=黄 / それ以降=端末の既定色 |
| ライブ取得の失敗継続 | 赤（警告アイコン + 失敗が続いている時間） |
| TTL 超過の経過時間 | 灰 dim（状態の色はそのまま） |
| 列ヘッダ・選択カードのボーダー | 列の `color` |

### 詳細モーダル（`Enter`）

項目を `↑↓` で選び `Enter` で編集する、の 1 文法だけで動く。項目は タイトル / ステータス / 期限 / note / 各リンク / ＋リンクを追加 / 各セッション / ＋セッションを紐づける。

- **ステータス**: 選択中に `←→` でその場切替（`Enter` ならセレクタを開く）
- **note**: `Enter` でエディタを開く（config の `editor` / `$VISUAL` / `$EDITOR`。board は一時的に退避する）
- **リンク行**: `Enter` でリンクメモの編集、`Delete` で確認付きの解除
- **＋リンクを追加**: 空白・改行区切りで複数 URL を一括登録できる（1 つでも不正なら全体を中止する）
- **セッション行**: `Enter` でそのセッションへジャンプ、`Delete` で確認付きの解除
- **＋セッションを紐づける**: herdr が検出しているエージェント一覧から `↑↓` で選ぶ。herdr 不達時は無効表示になる
- `Esc` で board へ戻る。ただし `link-pane` がセッション紐づけ済みタスクの詳細を直接開いた場合（前述）は、`Esc` で board へは戻らずポップアップごと終了する

### 追加モーダル（`a`）

詳細モーダルと同じ項目リスト（タイトル / ステータス / 期限 / note / リンク）だが、**フォーカス中の項目はそのまま入力できる**（`Enter` で開く操作は不要）。

- `↑↓` で項目移動。移動しても入力内容は保持される
- ステータスは現在フォーカスしている列が既定。`←→` で切替
- **`Enter` はどの項目にいても「作成して閉じる」**（タイトルが空ならエラーを出して閉じない）。`Esc` で取消
- タイトルに複数行を貼り付けると **1 行＝1 タスク**として一括作成する（他の項目の値は全行に適用される）
- `Enter` は作成に使うため、**改行は `Shift+Enter` / `Alt+Enter` / `Ctrl+J`** で入れる。`Shift+Enter` をそのまま判別できるのは拡張キーボードプロトコル対応端末だけだが、`shift+enter=text:\x1b\r` のように ESC+CR を送る設定の端末（ghostty など）では `Alt+Enter` として届くため、こちらも改行として受理する。タイトルの改行は 1 行＝1 タスク、note の改行はそのまま複数行 note になる。どちらのキーが有効かはフッタのキーヘルプに表示される

### 起動モーダル（セッションが紐づいていないタスクで `g`）

作業ディレクトリの候補（既存セッションの cwd を頻度順、同率なら直近リンク順、それも同じなら辞書順）と、初期プロンプトの編集欄を持つ。

- `Tab` で作業ディレクトリ欄とプロンプト欄を切り替える
- 作業ディレクトリ欄は `↑↓` で候補を選ぶ。最後の行（入力する）を選ぶと自由入力になる
- プロンプト欄は複数行編集できる。**`Enter` はどこにいても起動する**ため、改行は追加モーダルと同じキー（`Shift+Enter` / `Alt+Enter` / `Ctrl+J`）で入れる
- `Ctrl+Y` でプロンプトをクリップボードへコピーする（OSC 52。対応端末でのみ反映され、成功の確認手段は無い）
- `Enter` で起動する。起動は pane 作成 → agent 起動 → セッション id の検出待ち → tasks.json への紐づけ → プロンプト送信の順に進み、モーダルはすぐ閉じてステータス行に進捗と結果を出す
- 途中で失敗しても **pane は自動で閉じない**。以後の状況は詳細モーダルの ＋セッションを紐づける から手動で追いつける
- `Esc` でモーダルを閉じる（起動開始後は、ボードに戻ってから `Esc` で起動そのものを中止できる）

### 入力欄について

- **ペースト**: ブラケットペーストに対応しているので、URL やタスク一覧はターミナルからそのまま貼り付けられる
- **日本語入力**: IME の確定文字列はコマンドキーと解釈せず常に入力欄へ入る（確定文字列が飲み込まれない）。入力欄を持たない画面（確認ダイアログ・セレクタ）でも、複数文字がまとめて確定したイベントはキー名として解釈しない

## 開発

```
go build ./...          # ビルド
go test ./...           # テスト（実ユーザーデータには触れない）
go test -race ./...     # 競合検査つき
gofmt -l . && go vet ./...
```

ローカルの変更を herdr で試す場合:

```
go build -o bin/taskherd ./cmd/taskherd
herdr plugin link .     # link は build を実行しないので、先に自分でビルドする
```

`internal/` の構成:

| パッケージ | 役割 |
|---|---|
| `model` | タスクのデータモデル・検証・URL 種別判別 |
| `store` | tasks.json の排他（flock）・原子書き込み・変更監視 |
| `config` | config.toml の読み込みとパス解決 |
| `herdrc` | herdr の snapshot 取得・イベント購読・pane 操作 |
| `fetch` | gh / Jira によるライブ状態取得と cache.json |
| `tui` | bubbletea v2 の board / 詳細・追加モーダル / picker |
| `cli` | cobra のコマンド定義と `--json` 出力 |

## ライセンス

MIT License（[LICENSE](LICENSE)）
