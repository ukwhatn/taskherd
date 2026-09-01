package i18n

// cliJA is the Japanese command line text. The register is the one described on jaCatalog: help
// summaries and flag descriptions are noun phrases or dictionary-form verbs, everything that is a
// sentence is 敬体, and a refusal says what to do rather than only what went wrong.
var cliJA = CLI{
	Root: CLIRoot{
		Short:           "herdr のエージェントセッション・PR・チケットをタスク単位で束ねるタスク管理ツール",
		FlagJSON:        "結果を JSON で stdout に出力する（対話は行わない）",
		FlagNotifyError: "失敗したときに herdr の通知でこのラベルを知らせる",
		ErrorPrefix:     "エラー: %v\n",
		HintPrefix:      "ヒント: %s\n",
		NotifyTitle:     "taskherd: %sに失敗",
		NotifyBody:      "%s（%s）",
		ConfirmPrompt:   "%s [y/N]: ",
		Cancelled:       "中止しました",
		BadTaskID: Problem{
			Msg:  "タスク id が不正です: %q",
			Hint: "id は正の整数で指定します（#12 表記も使えます）",
		},
		UnknownColumn: Problem{
			Msg:  "未定義の列 id です: %q",
			Hint: "有効な列 id: %s",
		},
		BadDueHint: "例: --due 2026-08-31",
		BadURL: Problem{
			Msg:  "URL が不正です: %q",
			Hint: "スキームとホストを含む URL を指定します（例: https://github.com/owner/repo/pull/1）",
		},
		TokenFileUnreadable: "token_file %q を読めません: %v",
		TokenFileEmpty:      "token_file %q が空です",
	},
	Task: CLITask{
		AddShort:       "タスクを作成する",
		Created:        "#%d を作成しました（%s）: %s",
		AddFlagStatus:  "作成時の列 id（既定: config の先頭列）",
		AddFlagDue:     "期限（YYYY-MM-DD）",
		AddFlagNote:    "note の初期値",
		AddFlagLink:    "紐づける外部リンク URL（複数指定可）",
		AddFlagSession: "紐づけるセッション（current または UUID）",
		AddFlagCwd:     "セッションの作業ディレクトリ（--session が UUID で herdr が解決できない場合は必須）",

		ListShort:      "タスク一覧を表示する（既定は terminal 列を除く）",
		ListEmpty:      "該当するタスクがありません",
		ListFlagStatus: "表示する列 id（複数指定可。未定義の列 id も指定できる）",
		ListFlagAll:    "terminal 列も表示する",

		ShowShort: "タスクの詳細を表示する",

		EditShort: "タスクの属性を更新する",
		EditNothing: Problem{
			Msg:  "更新する項目が指定されていません",
			Hint: `--title / --due / --status のいずれかを指定します（--due "" で期限を消せます）`,
		},
		Edited:         "#%d を更新しました（%s）: %s",
		EditFlagTitle:  "新しいタイトル",
		EditFlagDue:    "新しい期限（YYYY-MM-DD。空文字で削除）",
		EditFlagStatus: "新しい列 id",

		MoveShort: "タスクを別の列へ移動する",
		DoneShort: "タスクを done 列へ移動する（move <id> done の alias）",
		Moved:     "#%d を %s へ移動しました: %s",

		RmShort: "タスクを削除する",
		RmNeedsYes: Problem{
			Msg:  "削除の確認が必要です",
			Hint: "--yes を指定します（--json では確認プロンプトを出しません）",
		},
		RmConfirm: "#%d「%s」を削除しますか？",
		Removed:   "#%d を削除しました: %s",
		RmFlagYes: "確認プロンプトを省略する",

		FetchFailed:        "取得に失敗しました: %s",
		NotFetched:         "未取得（refresh で取得できます）",
		Live:               "%s（%s 前）",
		LiveStale:          "%s（%s 前 / TTL 超過）",
		LastFetchFailed:    " 最新の取得は失敗しました: %s",
		UnknownColumnLabel: "未定義の列",
		EmptyList:          "  （なし）\n",
	},
	Link: CLILink{
		LinkShort:   "外部リンクを紐づける（種別は URL から自動判別）",
		FlagNote:    "リンク単位のメモ",
		Linked:      "#%d に [%s] %s を紐づけました",
		UnlinkShort: "外部リンクの紐づけを外す",
		Unlinked:    "#%d から %s の紐づけを外しました",
	},
	Note: CLINote{
		Short: "note を編集する（既定は $EDITOR で開く）",
		BothSetAndAppend: Problem{
			Msg:  "--set と --append は同時に指定できません",
			Hint: "上書きは --set、追記は --append のどちらか一方を指定します",
		},
		Updated:    "#%d の note を更新しました",
		FlagSet:    "note を指定文字列で上書きする",
		FlagAppend: "note に追記する",
		NothingToSet: Problem{
			Msg:  "note の編集内容が指定されていません",
			Hint: "--set か --append を指定します（--json では $EDITOR を起動しません）",
		},
		NoEditor: Problem{
			Msg:  "エディタが設定されていません",
			Hint: "config.toml の editor か $EDITOR を設定します。--set / --append なら非対話でも指定できます",
		},
		EditorFailed: "%s を起動できません: %w",
	},
	Session: CLISession{
		Short:     "タスクとエージェントセッションの紐づけを操作する",
		LinkShort: "エージェントセッションをタスクに紐づける",
		AmbiguousSpec: Problem{
			Msg:  "セッションの指定方法が 1 つに定まりません",
			Hint: "--current / --session-id <uuid> / --pane <pane_id> のいずれか 1 つを指定します",
		},
		Linked:        "#%d に %s セッション %s を紐づけました",
		FlagCurrent:   "この pane で動いているセッションを紐づける",
		FlagSessionID: "セッション UUID を明示指定する",
		FlagPane:      "対象 pane_id を明示指定する",
		FlagCwd:       "セッションの作業ディレクトリ（herdr が解決できない場合は必須）",
		FlagLabel:     "セッションの表示名",

		UnlinkShort: "セッションの紐づけを外す",
		Unlinked:    "#%d からセッション %s の紐づけを外しました",

		NoCurrentPane: Problem{
			Msg:  "現在の pane を特定できません（HERDR_PANE_ID がありません）",
			Hint: "herdr 管理 pane の外では --session-id <uuid> --cwd <path> で明示指定します",
		},
		NoAgentInPane: Problem{
			Msg:  "pane %s でエージェントを検出できません",
			Hint: "エージェントが動いている pane を指定するか、--session-id <uuid> --cwd <path> で明示指定します",
		},
		NoSessionID: Problem{
			Msg:  "pane %s のセッション ID を検出できません",
			Hint: "--session-id <uuid> で明示指定するか、herdr integration install claude を実行してから再試行します",
		},
		Unresolvable: Problem{
			Msg:  "セッション %s を herdr 上で解決できません",
			Hint: "--cwd <path> でセッションの作業ディレクトリを指定します（resume に必要なため省略できません）",
		},
		ReportFailed:  "注記: pane %s へのタスク id 記録に失敗しました: %v\n",
		HerdrDownNote: "注記: herdr に接続できないためセッション状態を表示しません（%v）\n",
	},
	Jump: CLIJump{
		Short:        "紐づいたセッションへ移動する（消滅している場合は resume 起動）",
		FlagSession:  "移動先のセッション UUID（複数紐づいている場合に指定）",
		FlagYes:      "resume 起動の確認プロンプトを省略する",
		FlagNewSpace: "新しい space を作って resume 起動する（値はラベル。生存 pane への移動では無視される）",
		NoSession: Problem{
			Msg:  "#%d にセッションが紐づいていません",
			Hint: "taskherd session link %d --current で紐づけます",
		},
		NotLinked: Problem{
			Msg:  "#%d にセッション %s は紐づいていません",
			Hint: "taskherd show %d で紐づいているセッションを確認できます",
		},
		TooMany: Problem{
			Msg:  "#%d には %d 件のセッションが紐づいています",
			Hint: "--session <uuid> で移動先を指定します（候補: %s）",
		},
		ChooseHeader: "#%d に紐づくセッション:\n",
		ChoosePrompt: "移動先の番号",
		BadChoice: Problem{
			Msg:  "番号が不正です: %q",
			Hint: "1〜%d の番号を入力します（--session <uuid> でも指定できます）",
		},
		UnsupportedResume: Problem{
			Msg:  "%s セッションの pane がありません。この agent の再開には未対応です",
			Hint: "%s で手動で再開します",
		},
		NeedsYes: Problem{
			Msg:  "pane がないため resume 起動の確認が必要です",
			Hint: "--yes を指定します（--json では確認プロンプトを出しません）",
		},
		ConfirmResume: "pane がありません。%s で claude --resume を起動しますか？",
		HerdrDownClaude: Problem{
			Msg:  "herdr に接続できないため移動できません: %v",
			Hint: "cd %s && claude --resume %s を実行してください",
		},
		HerdrDownOther: Problem{
			Msg:  "herdr に接続できないため移動できません: %v",
			Hint: "%s で %s セッションを手動で再開してください",
		},
		Moved:        "#%d のセッションへ移動しました（pane %s）\n",
		Resumed:      "#%d のセッションを pane %s で再開しました\n",
		WaitingInput: "起動直後に入力待ちになっています。pane を開いて応答してください",
	},
	Start: CLIStart{
		Short:            "タスクに新しいエージェントセッションを起こす",
		FlagCwd:          "起動する作業ディレクトリ（候補が定まらなければ必須）",
		FlagPrompt:       "起動直後に送るプロンプト（省略時は config のテンプレートを使う。空文字を明示すると送らない）",
		FlagNew:          "前回起動した agent があっても回収せず、新しく起こす（NAME に連番が付く）",
		FlagNoFocus:      "起こした pane へ移動しない（既定は移動する）",
		FlagSpace:        "起動先の space（herdr の workspace id）。既定は現在の space",
		FlagNewSpace:     "新しい space を作ってそこで起動する（値はラベル。空なら herdr が名前を決める）",
		CandidatesHeader: "作業ディレクトリの候補:",
		ChoosePrompt:     "番号か、パスを直接入力",
		BlankCwd: Problem{
			Msg:  "--cwd が空白だけです",
			Hint: "作業ディレクトリを指定するか、--cwd 自体を省略して候補から選びます",
		},
		NoCandidate: Problem{
			Msg:  "cwd の候補がありません（このタスクに紐づくセッションがまだありません）",
			Hint: "--cwd <path> で作業ディレクトリを指定します",
		},
		ManyCandidates: Problem{
			Msg:  "cwd の候補が複数あります",
			Hint: "--cwd <path> で作業ディレクトリを指定します（候補: %s）",
		},
		BadChoice: Problem{
			Msg:  "番号が不正です: %q",
			Hint: "1〜%d の番号を入力するか、パスを直接入力します",
		},
		EmptyCwd: Problem{
			Msg:  "作業ディレクトリが空です",
			Hint: "パスを入力するか --cwd で指定します",
		},
		ReusableBusy: Problem{
			Msg:  "#%d の前回の起動が pane %s に残っています（まだ使える状態ではありません）",
			Hint: "pane %s を確認してください",
		},
		AlreadyLinked: Problem{
			Msg:  "#%d は既にこのセッションに紐づいています",
			Hint: "taskherd jump %d で移動できます",
		},
		OtherCwd: Problem{
			Msg:  "#%d の前回の起動が別の cwd（%s）の pane %s で動いています",
			Hint: "pane %s へ移るか、taskherd start %d --new --cwd %s で新しく起こします",
		},
		OtherSpace: Problem{
			Msg:  "#%d の前回の起動が別の space（%s）の pane %s で動いています",
			Hint: "pane %s へ移るか、space を指定せずに taskherd start %d を実行してその pane を回収します",
		},
		SpaceConflict: Problem{
			Msg:  "--space と --new-space は同時に指定できません",
			Hint: "既存の space に立てるなら --space、新しい space を作るなら --new-space だけを指定します",
		},
		PartialLabel:      "start は起動を開始したあとに失敗しました（結果は stdout に出力済み）",
		StartFailed:       "pane %s を確認してください（起動に失敗しました）",
		WaitingInput:      "起動直後に入力待ちになっています（%s）",
		WaitingInputHint:  "pane %s を開いて応答してから、セッション picker で後から紐づけます",
		CheckPaneHint:     "pane %s を確認し、セッション picker で後から紐づけます",
		TrustPrompt:       "入力待ちで止まっています（trust-folder の確認など）",
		NoSessionReported: "herdr がセッション id を報告しませんでした",
		LinkManuallyHint:  "pane %s / session %s を taskherd session link で手動で紐づけます",
		LinkFailedHint:    "pane %s を確認し、セッション picker で後から紐づけます",
		PromptFailedHint:  "起動と紐づけは済んでいます。プロンプトの送信だけ失敗しました",
		DoneWithPrompt:    " まで起動しました（プロンプト送信済み）",
		DoneWithoutPrompt: " まで起動しました（紐づけ済み、プロンプトは送っていません）",
		LaunchLabel:       "#%d の起動",
		ResumeLabel:       "#%d の再開",
	},
	Refresh: CLIRefresh{
		Short: "リンクのライブ取得を即時実行しキャッシュを更新する",
		NoTarget: Problem{
			Msg:  "取得対象が指定されていません",
			Hint: "id を指定するか --all を付けます",
		},
		BothIDAndAll: Problem{
			Msg:  "id と --all は同時に指定できません",
			Hint: "対象を絞るなら id のみ、全体を更新するなら --all のみを指定します",
		},
		FlagAll:       "全タスクのリンクを取得する",
		Refreshed:     "%d 件を更新しました",
		FailedSuffix:  "（%d 件失敗）",
		GitHubLimited: "GitHub のレート制限のため残りの取得を中断しました",
		JiraLimited:   "Jira のレート制限のため残りの取得を中断しました",
	},
	Update: CLIUpdate{
		Short:       "新しいリリースを確認して更新する",
		FlagCheck:   "確認だけ行い更新しない",
		FlagYes:     "確認プロンプトを出さずに更新する",
		Available:   "%s が公開されています（現在 %s）\n",
		UpToDate:    "最新です（%s）\n",
		Confirm:     "%s に更新しますか",
		Downloading: "%s をダウンロードしています…\n",
		Done:        "%s から %s に更新しました: %s\n",
		NotReleased: Problem{
			Msg:  "リリース版ではないため更新できません",
			Hint: "このバイナリはソースからビルドされています。git pull して再ビルドしてください",
		},
		NoRelease: Problem{
			Msg:  "公開されているリリースがありません",
			Hint: "最初のリリースが出るまで更新できません",
		},
		NotWritable: Problem{
			Msg:  "%s を書き換える権限がありません",
			Hint: "install.sh を実行し直すか、書き込める場所へ導入し直してください",
		},
		Failed: Problem{
			Msg:  "更新に失敗しました: %s",
			Hint: "元のバイナリはそのまま残っています。時間をおいて再実行してください",
		},
		Notice: "新しい版 %s が公開されています（現在 %s）。taskherd update で更新できます\n",
	},
	Version: CLIVersion{
		Short: "バージョンを表示する",
	},
	Config: CLIConfig{
		Short:     "設定ファイルとデータファイルを扱う",
		PathShort: "config・データファイルのパスを表示する",
		InitShort: "既定の config.toml を生成する",
		Exists: Problem{
			Msg:  "%s は既に存在します",
			Hint: "既存の設定を残すため上書きしません。作り直す場合は退避してから再実行します",
		},
		Created: "%s を作成しました\n",
	},
	Board: CLIBoard{
		Short: "kanban ボードを開く",
		NoTTY: Problem{
			Msg:  "board は対話 TUI のため --json では起動できません",
			Hint: "機械可読な出力は list --json / show --json を使います",
		},
		NoOpenColumn: Problem{
			Msg:  `board を開くには kind = "open" の列が最低 1 つ必要です`,
			Hint: "config.toml の [[columns]] に open の列を足します（場所は taskherd config path）",
		},
	},
	Plugin: CLIPlugin{
		Short:          "herdr プラグインの action から呼ばれる内部コマンド",
		OpenBoardShort: "board pane を開く（herdr-plugin.toml の open-board action の実体）",
		LinkPaneShort:  "この pane をタスクに紐づける picker を開く（herdr-plugin.toml の link-pane action の実体）",
		NoCallerPane: Problem{
			Msg:  "action の呼び出し元 pane を特定できません",
			Hint: "この action は pane を対象にした操作からのみ呼び出せます",
		},
	},
	Picker: CLIPicker{
		Short: "pane をタスクに紐づける選択 TUI（herdr-plugin.toml の picker entrypoint 専用）",
		NoTargetPane: Problem{
			Msg:  "%s が設定されていません",
			Hint: "picker は herdr プラグインの link-pane action からのみ起動します",
		},
	},
}
