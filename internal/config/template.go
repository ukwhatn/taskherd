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

[jira]
# site = "your-tenant.atlassian.net"
# email = "you@example.com"
# API トークンはこの環境変数から読む（config への平文保存はしない）
token_env = "TASKHERD_JIRA_TOKEN"
`

// DefaultFileContent returns the generated config.toml content.
func DefaultFileContent() string {
	return defaultFileContent
}
