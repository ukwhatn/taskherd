package config

// defaultFileContent is what config init writes; it must describe the same settings as Default().
// Environment specific values (GHES hosts, Jira site, address) stay commented out as examples.
const defaultFileContent = `# taskherd 設定ファイル
# 生成: taskherd config init / パス上書き: 環境変数 TASKHERD_CONFIG

# note 編集に使うエディタ。解決順は editor > $VISUAL > $EDITOR
# herdr プラグインの pane は shell を経由せず環境変数が届かないことがあるため、
# board から note を編集するならここで指定しておくのが確実
# editor = "nano"

[board]
# ライブ取得の背景更新間隔（分）。0 で無効
refresh_interval_minutes = 10
# ライブ取得キャッシュの有効期間（分）
cache_ttl_minutes = 5
# カード・フッタのアイコン表記
#   "nerd"  : Nerd Font のグリフを使う（既定。端末フォントに Nerd Font が必要）
#   "ascii" : ASCII 記号で代替する
#   "none"  : 記号を使わず状態を単語で書く
icons = "nerd"
# リンク行を OSC 8 でハイパーリンクにする。対応端末ではクリックでブラウザが開く。
# 非対応端末では通常のテキストとして表示される
hyperlinks = true

# kanban の列。配列順が表示順になる。
# kind = "open" | "terminal"（terminal は board で折り畳み、list の既定表示から除外）
[[columns]]
id = "todo"
label = "ToDo"
kind = "open"
color = "gray"

[[columns]]
id = "planning"
label = "Planning"
kind = "open"
color = "blue"

[[columns]]
id = "working"
label = "Working"
kind = "open"
color = "green"

[[columns]]
id = "review"
label = "Review"
kind = "open"
color = "magenta"

[[columns]]
id = "done"
label = "Done"
kind = "terminal"
color = "purple"

[[columns]]
id = "wontfix"
label = "Wontfix"
kind = "terminal"
color = "gray"

[github]
# GitHub Enterprise Server のホスト（リンク種別の判別に使う）
# ghes_hosts = ["github.example.com"]

# ライブ取得に使う gh アカウント。gh の active account に依存せず取得したい場合に指定する。
# キーは "<host>" または "<host>/<owner>" で、解決順は owner 完全一致 > ホスト > gh の active account。
# 同一ホストに個人リポジトリと組織リポジトリが混在する場合、アカウントごとに見えるリポジトリが
# 違うため、owner 単位で指定しないと片方が 404 になる。
# 指定したキーは "gh auth token --hostname <host> --user <account>" で取得したトークンを
# gh サブプロセスにだけ渡す（トークンは config にも cache にも保存しない）。
# [github.accounts]
# "github.com/your-account" = "your-account"
# "github.com/some-org" = "work-account"
# "github.example.com" = "your-enterprise-account"

[jira]
# site = "your-tenant.atlassian.net"
# email = "you@example.com"
# API トークンはこの環境変数から読む（config への平文保存はしない）
token_env = "TASKHERD_JIRA_TOKEN"
# 環境変数が空のときは、このファイルからトークンを読む。先頭の ~/ は HOME に展開する。
# herdr プラグインとして board を開くと herdr サーバの環境を継承するためシェルの環境変数は
# 届かない。3 台で使う・board をプラグインから開く場合はこちらを設定する（chmod 600 推奨）
# token_file = "~/.config/taskherd/jira_token"
`

// DefaultFileContent returns the generated config.toml content.
func DefaultFileContent() string {
	return defaultFileContent
}
