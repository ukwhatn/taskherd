package i18n

// cliEN is the English command line text. Help summaries are lowercase verb phrases, the way
// cobra's own output reads; messages are sentence case with no closing period.
var cliEN = CLI{
	Root: CLIRoot{
		Short:           "Track tasks alongside the herdr agent sessions, PRs and tickets they belong to",
		FlagJSON:        "write the result to stdout as JSON (never prompts)",
		FlagNotifyError: "name this operation in a herdr notification if it fails",
		ErrorPrefix:     "error: %v\n",
		HintPrefix:      "hint: %s\n",
		NotifyTitle:     "taskherd: %s failed",
		NotifyBody:      "%s (%s)",
		ConfirmPrompt:   "%s [y/N]: ",
		Cancelled:       "Cancelled",
		BadTaskID: Problem{
			Msg:  "Not a task id: %q",
			Hint: "Give a positive integer (the #12 form works too)",
		},
		UnknownColumn: Problem{
			Msg:  "No column has the id %q",
			Hint: "Defined column ids: %s",
		},
		BadDueHint: "For example: --due 2026-08-31",
		BadURL: Problem{
			Msg:  "Not a URL: %q",
			Hint: "Include the scheme and host (for example https://github.com/owner/repo/pull/1)",
		},
		TokenFileUnreadable: "cannot read token_file %q: %v",
		TokenFileEmpty:      "token_file %q is empty",
	},
	Task: CLITask{
		AddShort:       "create a task",
		Created:        "Created #%d in %s: %s",
		AddFlagStatus:  "column id to create it in (default: the first column in the config)",
		AddFlagDue:     "due date (YYYY-MM-DD)",
		AddFlagNote:    "initial note",
		AddFlagLink:    "link URL to attach (repeatable)",
		AddFlagSession: "session to attach (current, or a UUID)",
		AddFlagCwd:     "the session's working directory (required when --session is a UUID herdr cannot resolve)",

		ListShort:      "list tasks (terminal columns are left out by default)",
		ListEmpty:      "No matching task",
		ListFlagStatus: "column ids to show (repeatable; ids no column defines are allowed)",
		ListFlagAll:    "include terminal columns",

		ShowShort: "show one task in full",

		EditShort: "update a task's fields",
		EditNothing: Problem{
			Msg:  "No field to update was given",
			Hint: `Give one of --title / --due / --status (--due "" clears the due date)`,
		},
		Edited:         "Updated #%d in %s: %s",
		EditFlagTitle:  "new title",
		EditFlagDue:    "new due date (YYYY-MM-DD, empty to clear)",
		EditFlagStatus: "new column id",

		MoveShort: "move a task to another column",
		DoneShort: "move a task to the done column (alias for move <id> done)",
		Moved:     "Moved #%d to %s: %s",

		RmShort: "delete a task",
		RmNeedsYes: Problem{
			Msg:  "Deleting needs a confirmation",
			Hint: "Pass --yes (--json never prompts)",
		},
		RmConfirm: "Delete #%d %q?",
		Removed:   "Deleted #%d: %s",
		RmFlagYes: "skip the confirmation prompt",

		FetchFailed:        "fetch failed: %s",
		NotFetched:         "not fetched (refresh reads it)",
		Live:               "%s (%s ago)",
		LiveStale:          "%s (%s ago, TTL expired)",
		LastFetchFailed:    " last refresh failed: %s",
		UnknownColumnLabel: "undefined column",
		EmptyList:          "  (none)\n",
	},
	Link: CLILink{
		LinkShort:   "attach a link (the kind is read from the URL)",
		FlagNote:    "note for this link",
		Linked:      "Attached [%[2]s] %[3]s to #%[1]d",
		UnlinkShort: "remove a link",
		Unlinked:    "Removed %[2]s from #%[1]d",
	},
	Note: CLINote{
		Short: "edit a task's note (opens $EDITOR by default)",
		BothSetAndAppend: Problem{
			Msg:  "--set and --append cannot be given together",
			Hint: "Use --set to replace the note, or --append to add to it",
		},
		Updated:    "Updated the note of #%d",
		FlagSet:    "replace the note with this text",
		FlagAppend: "append this text to the note",
		NothingToSet: Problem{
			Msg:  "No note text was given",
			Hint: "Give --set or --append (--json never opens $EDITOR)",
		},
		NoEditor: Problem{
			Msg:  "No editor is configured",
			Hint: "Set editor in config.toml or $EDITOR. --set / --append work without one",
		},
		EditorFailed: "cannot start %s: %w",
	},
	Session: CLISession{
		Short:     "attach and detach agent sessions",
		LinkShort: "attach an agent session to a task",
		AmbiguousSpec: Problem{
			Msg:  "More than one way of naming the session was given",
			Hint: "Give exactly one of --current / --session-id <uuid> / --pane <pane_id>",
		},
		Linked:        "Attached %[2]s session %[3]s to #%[1]d",
		FlagCurrent:   "attach the session running in this pane",
		FlagSessionID: "name the session by UUID",
		FlagPane:      "name the pane by pane_id",
		FlagCwd:       "the session's working directory (required when herdr cannot resolve it)",
		FlagLabel:     "display name for the session",

		UnlinkShort: "detach a session",
		Unlinked:    "Detached session %[2]s from #%[1]d",

		NoCurrentPane: Problem{
			Msg:  "Cannot tell which pane this is (HERDR_PANE_ID is not set)",
			Hint: "Outside a herdr pane, name the session with --session-id <uuid> --cwd <path>",
		},
		NoAgentInPane: Problem{
			Msg:  "No agent detected in pane %s",
			Hint: "Name a pane with an agent in it, or give --session-id <uuid> --cwd <path>",
		},
		NoSessionID: Problem{
			Msg:  "Cannot detect a session id in pane %s",
			Hint: "Give --session-id <uuid>, or run herdr integration install claude and retry",
		},
		Unresolvable: Problem{
			Msg:  "herdr cannot place session %s",
			Hint: "Give --cwd <path> for the session's working directory (a resume cannot do without it)",
		},
		ReportFailed:  "note: could not record the task id on pane %s: %v\n",
		HerdrDownNote: "note: session state is left out because herdr is unreachable (%v)\n",
	},
	Jump: CLIJump{
		Short:       "go to a linked session (resuming it when the pane is gone)",
		FlagSession: "session UUID to go to (when several are linked)",
		FlagYes:     "skip the confirmation before resuming",
		NoSession: Problem{
			Msg:  "No session is attached to #%d",
			Hint: "Attach one with taskherd session link %d --current",
		},
		NotLinked: Problem{
			Msg:  "Session %[2]s is not attached to #%[1]d",
			Hint: "taskherd show %d lists the attached sessions",
		},
		TooMany: Problem{
			Msg:  "#%d has %d sessions attached",
			Hint: "Name one with --session <uuid> (attached: %s)",
		},
		ChooseHeader: "Sessions attached to #%d:\n",
		ChoosePrompt: "Number to go to",
		BadChoice: Problem{
			Msg:  "Not one of the numbers: %q",
			Hint: "Enter a number from 1 to %d (--session <uuid> works too)",
		},
		UnsupportedResume: Problem{
			Msg:  "The pane of the %s session is gone, and resuming this agent is not supported",
			Hint: "Resume it by hand in %s",
		},
		NeedsYes: Problem{
			Msg:  "The pane is gone, so resuming needs a confirmation",
			Hint: "Pass --yes (--json never prompts)",
		},
		ConfirmResume: "The pane is gone. Run claude --resume in %s?",
		HerdrDownClaude: Problem{
			Msg:  "Cannot jump while herdr is unreachable: %v",
			Hint: "Run: cd %s && claude --resume %s",
		},
		HerdrDownOther: Problem{
			Msg:  "Cannot jump while herdr is unreachable: %v",
			Hint: "Resume the %[2]s session by hand in %[1]s",
		},
		Moved:        "Moved to the session of #%d (pane %s)\n",
		Resumed:      "Resumed the session of #%d in pane %s\n",
		WaitingInput: "It stopped on a prompt right after starting. Open the pane and answer it",
	},
	Start: CLIStart{
		Short:            "start a new agent session for a task",
		FlagCwd:          "working directory to start in (required when the candidates are ambiguous)",
		FlagPrompt:       "prompt to send once it starts (default: the config template; an explicit empty string sends none)",
		FlagNew:          "start a new agent even when the previous one is still around (NAME gets a suffix)",
		FlagNoFocus:      "do not move to the new pane (the default is to move)",
		CandidatesHeader: "Working directory candidates:",
		ChoosePrompt:     "Number, or a path",
		BlankCwd: Problem{
			Msg:  "--cwd is only whitespace",
			Hint: "Give a working directory, or leave --cwd out and pick from the candidates",
		},
		NoCandidate: Problem{
			Msg:  "No working directory to start in (this task has no session yet)",
			Hint: "Give one with --cwd <path>",
		},
		ManyCandidates: Problem{
			Msg:  "Several working directories are possible",
			Hint: "Name one with --cwd <path> (candidates: %s)",
		},
		BadChoice: Problem{
			Msg:  "Neither a number nor a path: %q",
			Hint: "Enter a number from 1 to %d, or a path",
		},
		EmptyCwd: Problem{
			Msg:  "The working directory is empty",
			Hint: "Enter a path, or give one with --cwd",
		},
		ReusableBusy: Problem{
			Msg:  "The previous launch of #%d is still in pane %s and is not ready to reuse",
			Hint: "Look at pane %s",
		},
		AlreadyLinked: Problem{
			Msg:  "#%d is already attached to this session",
			Hint: "taskherd jump %d goes to it",
		},
		OtherCwd: Problem{
			Msg:  "The previous launch of #%[1]d is running in pane %[3]s under a different cwd (%[2]s)",
			Hint: "Move to pane %[1]s, or start a new one with taskherd start %[2]d --new --cwd %[3]s",
		},
		PartialLabel:      "start failed after the launch had begun (the result is already on stdout)",
		StartFailed:       "Look at pane %s (the launch failed)",
		WaitingInput:      "it stopped on a prompt right after starting (%s)",
		WaitingInputHint:  "Open pane %s and answer it, then attach the session from the picker",
		CheckPaneHint:     "Look at pane %s, then attach the session from the picker",
		TrustPrompt:       "it is waiting on a prompt (a trust-folder confirmation, most likely)",
		NoSessionReported: "herdr never reported a session id",
		LinkManuallyHint:  "Attach pane %s / session %s by hand with taskherd session link",
		LinkFailedHint:    "Look at pane %s, then attach the session from the picker",
		PromptFailedHint:  "The launch and the link are done; only sending the prompt failed",
		DoneWithPrompt:    " reached (prompt sent)",
		DoneWithoutPrompt: " reached (attached, no prompt sent)",
		LaunchLabel:       "starting #%d",
		ResumeLabel:       "resuming #%d",
	},
	Refresh: CLIRefresh{
		Short: "refresh the live state of links now and update the cache",
		NoTarget: Problem{
			Msg:  "Nothing to refresh was named",
			Hint: "Give a task id, or pass --all",
		},
		BothIDAndAll: Problem{
			Msg:  "An id and --all cannot be given together",
			Hint: "Give the id alone for one task, or --all alone for every task",
		},
		FlagAll:       "refresh the links of every task",
		Refreshed:     "Refreshed %d",
		FailedSuffix:  " (failed %d)",
		GitHubLimited: "Stopped early: GitHub rate limit",
		JiraLimited:   "Stopped early: Jira rate limit",
	},
	Update: CLIUpdate{
		Short:       "Check for a newer release and install it",
		FlagCheck:   "Only check; do not install",
		FlagYes:     "Install without asking",
		Available:   "%s is available (running %s)\n",
		UpToDate:    "Up to date (%s)\n",
		Confirm:     "Update to %s?",
		Downloading: "Downloading %s…\n",
		Done:        "Updated %s to %s: %s\n",
		NotReleased: Problem{
			Msg:  "This is not a released build, so there is nothing to update it to",
			Hint: "It was built from source. Pull and rebuild instead",
		},
		NoRelease: Problem{
			Msg:  "Nothing has been released yet",
			Hint: "There is nothing to update to until the first release is published",
		},
		NotWritable: Problem{
			Msg:  "No permission to replace %s",
			Hint: "Run install.sh again, or reinstall somewhere you can write to",
		},
		Failed: Problem{
			Msg:  "The update failed: %s",
			Hint: "The binary you had is untouched. Try again later",
		},
		Notice: "%s is available (running %s). Run taskherd update to install it\n",
	},
	Version: CLIVersion{
		Short: "Show the version",
	},
	Config: CLIConfig{
		Short:     "work with the config and data files",
		PathShort: "print where the config and data files live",
		InitShort: "write a default config.toml",
		Exists: Problem{
			Msg:  "%s already exists",
			Hint: "It is left alone rather than overwritten. Move it aside and run this again to recreate it",
		},
		Created: "Created %s\n",
	},
	Board: CLIBoard{
		Short: "open the kanban board",
		NoTTY: Problem{
			Msg:  "board is an interactive TUI and cannot run under --json",
			Hint: "Use list --json / show --json for machine-readable output",
		},
		NoOpenColumn: Problem{
			Msg:  `board needs at least one column with kind = "open"`,
			Hint: "Add an open column to [[columns]] in config.toml (taskherd config path says where)",
		},
	},
	Plugin: CLIPlugin{
		Short:          "internal commands the herdr plugin actions call",
		OpenBoardShort: "open a board pane (what the open-board action in herdr-plugin.toml runs)",
		LinkPaneShort:  "open the picker that attaches this pane to a task (what the link-pane action runs)",
		NoCallerPane: Problem{
			Msg:  "Cannot tell which pane invoked this action",
			Hint: "This action can only be called from an operation that targets a pane",
		},
	},
	Picker: CLIPicker{
		Short: "the pane-to-task picker (for the picker entrypoint in herdr-plugin.toml only)",
		NoTargetPane: Problem{
			Msg:  "%s is not set",
			Hint: "The picker is only started from the herdr plugin's link-pane action",
		},
	},
}
