package config

// defaultFileContent は config init が書き出す内容。Default() と同じ設定を表す。
// 環境依存の値（GHES ホスト・Jira サイト・メールアドレス）は例としてコメントアウトしておく。
const defaultFileContent = `# taskherd 設定ファイル
# 生成: taskherd config init / パス上書き: 環境変数 TASKHERD_CONFIG

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

// DefaultFileContent は既定 config.toml の内容を返す。
func DefaultFileContent() string {
	return defaultFileContent
}
