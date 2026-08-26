package i18n

// errEN is the English text of the errors taskherd raises as types.
var errEN = Err{
	Task: ErrTask{
		TaskNotFound:    "No such task",
		LinkNotFound:    "No such link",
		LinkExists:      "That URL is already attached",
		EmptyTitle:      "The title is empty",
		EmptyStatus:     "The status is empty",
		SessionNotFound: "That session is not attached",
		SessionExists:   "That session is already attached to this task",
		EmptySessionID:  "The session id is empty",
		EmptySessionCwd: "The session's cwd is empty",
		EmptyAgent:      "The agent name is empty",
		BadDate:         "Dates are written YYYY-MM-DD: %q",
	},
	Data: ErrData{
		Invalid:         "%s failed validation (%d problems):",
		InvalidSubject:  "the input",
		Violation:       "  - %s: %s",
		VersionMismatch: "tasks.json is version %d (this binary reads %d)",

		Corrupt: Problem{
			Msg:  "cannot read %s: %v",
			Hint: "What was there before the write is still in %s. Look at it and restore by hand — taskherd will not overwrite it",
		},
		Version: Problem{
			Msg:  "cannot read %s: %v",
			Hint: "If the file is newer, update taskherd to a binary that reads that version. If it is older, migrate it by hand — restoring the backup will not help",
		},
		NoHome: Problem{
			Msg:  "HOME is not set",
			Hint: "Set HOME, or name XDG_STATE_HOME and TASKHERD_CONFIG explicitly",
		},
		Lock: Problem{
			Msg:  "could not take the lock on %s within %s: %v",
			Hint: "Another taskherd process may be writing. Wait for it to finish and run this again",
		},

		ColumnsEmpty:      "no column is defined",
		ColumnIDEmpty:     "id is empty",
		ColumnIDDuplicate: "id %q is already used by columns[%d]",
		ColumnLabelEmpty:  "label is empty",
		ColumnKindInvalid: "kind is %q or %q (got %q)",
		NextIDTooSmall:    "next_id must be a positive integer greater than max(id)=%d (got %d)",
		TaskIDNotPositive: "id must be a positive integer (got %d)",
		TaskIDDuplicate:   "id %d is already used by tasks[%d]",
		TaskDueFormat:     "must be written YYYY-MM-DD (got %q)",
		TimestampFormat:   "must be RFC 3339 (got %q)",
		IntervalNegative:  "must be 0 or more (0 disables background refresh; got %d)",
		CacheTTLNegative:  "must be 0 or more (got %d)",
		IconModeInvalid:   "must be nerd / ascii / none (got %q)",
		LanguageInvalid:   "must be one of %s (got %q)",
		AccountIncomplete: "both a host and an account name are needed (got %q = %q)",
		AccountKeyFormat:  `the key is written "<host>" or "<host>/<owner>" (got %q)`,
	},
	Live: ErrLive{
		GHNotFound: Problem{
			Msg:  "gh is not on PATH",
			Hint: "Install the GitHub CLI from https://cli.github.com/",
		},
		GHRateLimited: Problem{
			Msg:  "GitHub rate limit reached: %s",
			Hint: "Wait a while and try again (the rest of this cycle's GitHub fetches were stopped)",
		},
		GHFailed: Problem{
			Msg:  "gh failed",
			Hint: "Switch accounts with `gh auth switch --hostname <host>`, or name one under [github.accounts] in the config as \"<host>/<owner>\"",
		},
		GHAccountActive:       "Fetched as: gh's active account (nothing under [github.accounts] matched)",
		GHAccountActiveFailed: "Fetched as: gh's active account (%q = %q under [github.accounts] resolved to no token)",
		GHAccountNamed:        "Fetched as: %q (from %q under [github.accounts])",
		GHAccountOwnerHint: `The account this fetched as may not be able to see the repository at all. ` +
			`Name a per-owner account under [github.accounts] as "<host>/<owner>" (for example "github.com/some-org" = "work-account"). ` +
			`A host-level entry cannot resolve a host that holds both personal and organization repositories`,
		GHTokenFailed: "cannot get a token for account %[2]q on %[1]s named under github.accounts (continuing with gh's active account): %[3]s",
		GHTokenEmpty:  "gh returned no token for account %[2]q on %[1]s named under github.accounts (continuing with gh's active account)",

		JiraAuth: Problem{
			Msg:  "The Jira API token is not valid",
			Hint: "Atlassian expires tokens after a year from 2026. Reissue one at https://id.atlassian.com/manage-profile/security/api-tokens and update the environment variable",
		},
		JiraRateLimited: Problem{
			Msg:  "Jira rate limit reached",
			Hint: "Wait as long as Retry-After asks and try again (the rest of this cycle's Jira fetches were stopped)",
		},
		JiraStatus: Problem{
			Msg:  "Jira API returned %d: %s",
			Hint: "The body is Jira's own. Check the site value and the token's permissions",
		},
		JiraNotConfigured: Problem{
			Msg:  "Jira is not configured",
			Hint: "Set site and email under [jira] in config.toml, and put the token in the variable token_env names or the file token_file names",
		},
		JiraNotConfiguredWhy: "Jira is not configured: %s",
		NoJiraKey:            "cannot read a Jira issue key out of %s",
	},
	Herd: ErrHerdr{
		APICode:    "herdr error (%s)",
		APIMessage: "herdr error (%s): %s",
		Unavailable: Problem{
			Msg:  "cannot reach herdr (%s): %v",
			Hint: "herdr may not be running. Everything except the herdr features (session state, jump, --session current) works without it",
		},
	},
}
