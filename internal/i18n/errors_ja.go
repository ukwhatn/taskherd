package i18n

// errJA is the Japanese text of the errors taskherd raises as types.
var errJA = Err{
	Task: ErrTask{
		TaskNotFound:    "タスクが見つかりません",
		LinkNotFound:    "リンクが見つかりません",
		LinkExists:      "同じ URL が既に紐づいています",
		EmptyTitle:      "タイトルが空です",
		EmptyStatus:     "ステータスが空です",
		SessionNotFound: "セッションが紐づいていません",
		SessionExists:   "同じセッションが既にこのタスクに紐づいています",
		EmptySessionID:  "セッション ID が空です",
		EmptySessionCwd: "セッションの cwd が空です",
		EmptyAgent:      "エージェント名が空です",
		BadDate:         "日付は YYYY-MM-DD 形式で指定してください: %q",
	},
	Data: ErrData{
		Invalid:         "%s の検証に失敗しました（%d 件）:",
		InvalidSubject:  "入力",
		Violation:       "  - %s: %s",
		VersionMismatch: "tasks.json の version が %d です（このバイナリの対応は %d）",

		Corrupt: Problem{
			Msg:  "%s を読み込めません: %v",
			Hint: "書き込み前の内容は %s に残っています。内容を確認して手動で復旧してください（taskherd は自動で上書きしません）",
		},
		Version: Problem{
			Msg:  "%s を読み込めません: %v",
			Hint: "ファイルが新しい場合は taskherd を新しい version に対応したバイナリへ更新します。古い形式の場合は手動で移行します（バックアップからの復旧では解決しません）",
		},
		NoHome: Problem{
			Msg:  "HOME が設定されていません",
			Hint: "HOME を設定するか、XDG_STATE_HOME と TASKHERD_CONFIG を明示してください",
		},
		Lock: Problem{
			Msg:  "%s のロックを %s 以内に取得できませんでした: %v",
			Hint: "他の taskherd プロセスが書き込み中の可能性があります。完了を待ってから再実行してください",
		},

		ColumnsEmpty:      "列が 1 つも定義されていません",
		ColumnIDEmpty:     "id が空です",
		ColumnIDDuplicate: "id %q が columns[%d] と重複しています",
		ColumnLabelEmpty:  "label が空です",
		ColumnKindInvalid: "kind は %q か %q です（実際: %q）",
		NextIDTooSmall:    "next_id は max(id)=%d より大きい正の整数でなければなりません（実際: %d）",
		TaskIDNotPositive: "id は正の整数でなければなりません（実際: %d）",
		TaskIDDuplicate:   "id %d が tasks[%d] と重複しています",
		TaskDueFormat:     "YYYY-MM-DD 形式でなければなりません（実際: %q）",
		TimestampFormat:   "RFC 3339 形式でなければなりません（実際: %q）",
		IntervalNegative:  "0 以上でなければなりません（0 で背景更新を無効化。実際: %d）",
		CacheTTLNegative:  "0 以上でなければなりません（実際: %d）",
		IconModeInvalid:   "nerd / ascii / none のいずれかを指定します（実際: %q）",
		LanguageInvalid:   "%s のいずれかを指定します（実際: %q）",
		AccountIncomplete: "ホスト名とアカウント名の両方が必要です（実際: %q = %q）",
		AccountKeyFormat:  `キーは "<host>" または "<host>/<owner>" の形式で指定します（実際: %q）`,
	},
	Live: ErrLive{
		GHNotFound: Problem{
			Msg:  "gh コマンドが見つかりません",
			Hint: "https://cli.github.com/ から GitHub CLI (gh) を導入してください",
		},
		GHRateLimited: Problem{
			Msg:  "GitHub のレート制限に達しました: %s",
			Hint: "しばらく待ってから再試行してください（このサイクルの残りの GitHub 取得は中断しました）",
		},
		GHFailed: Problem{
			Msg:  "gh コマンドが失敗しました",
			Hint: "`gh auth switch --hostname <host>` でアカウントを切り替えるか、config の [github.accounts] に \"<host>/<owner>\" 形式でアカウントを指定します",
		},
		GHAccountActive:       "取得に使ったアカウント: gh の active account（config の [github.accounts] に一致する指定がありません）",
		GHAccountActiveFailed: "取得に使ったアカウント: gh の active account（config の [github.accounts] の %q = %q はトークンを解決できませんでした）",
		GHAccountNamed:        "取得に使ったアカウント: %q（config の [github.accounts] の %q）",
		GHAccountOwnerHint: `リポジトリが見えないアカウントで取得している可能性があります。` +
			`config の [github.accounts] に "<host>/<owner>" 形式で owner ごとのアカウントを指定してください（例: "github.com/some-org" = "work-account"）。` +
			`同一ホストに個人と組織が混在する場合、ホスト単位の指定では解決できません`,
		GHTokenFailed: "config の github.accounts が指定する %s のアカウント %q のトークンを取得できません（gh の active account で続行します）: %s",
		GHTokenEmpty:  "config の github.accounts が指定する %s のアカウント %q に対して gh がトークンを返しませんでした（gh の active account で続行します）",

		JiraAuth: Problem{
			Msg:  "Jira API token が無効です",
			Hint: "Atlassian は 2026 年からトークンを 1 年で失効させます。https://id.atlassian.com/manage-profile/security/api-tokens で再発行し、環境変数を更新してください",
		},
		JiraRateLimited: Problem{
			Msg:  "Jira のレート制限に達しました",
			Hint: "Retry-After の時間だけ待ってから再試行してください（このサイクルの残りの Jira 取得は中断しました）",
		},
		JiraStatus: Problem{
			Msg:  "Jira API が %d を返しました: %s",
			Hint: "本文は Jira の応答そのままです。site の値とトークンの権限を確認してください",
		},
		JiraNotConfigured: Problem{
			Msg:  "Jira の設定がありません",
			Hint: "config.toml の [jira] に site/email を設定し、token_env が指す環境変数か token_file が指すファイルにトークンを置いてください",
		},
		JiraNotConfiguredWhy: "Jira の設定がありません: %s",
		NoJiraKey:            "%s から Jira issue key を取り出せません",
	},
	Herd: ErrHerdr{
		APICode:    "herdr エラー (%s)",
		APIMessage: "herdr エラー (%s): %s",
		Unavailable: Problem{
			Msg:  "herdr に接続できません (%s): %v",
			Hint: "herdr が起動していない可能性があります。herdr 連携機能（セッション状態・jump・--session current）以外は herdr なしで動作します",
		},
	},
}
