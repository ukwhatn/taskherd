package config

// promptTemplateEN is the English built-in [session_start] prompt_template.
const promptTemplateEN = `Please work on #{{id}} {{title}}.

Current status: {{status}}

{{note}}
{{links}}`

// fileContentEN is what config init writes in English. It must describe the same settings as
// fileContentJA, key for key and default for default: the two are one file in two languages, and a
// setting documented in only one of them is invisible to half the users.
const fileContentEN = `# taskherd configuration
# Written by: taskherd config init / override the path with TASKHERD_CONFIG

# UI language, "ja" or "en". TASKHERD_LANG overrides this for one invocation
language = "en"

# The editor notes are edited in. Resolved as editor > $VISUAL > $EDITOR.
# A herdr plugin pane inherits the herdr server's environment rather than a shell's, so the
# variables may not arrive; name the editor here if you edit notes from the board
# editor = "nano"

[board]
# How often live state is refreshed in the background, in minutes. 0 turns it off
refresh_interval_minutes = 10
# How long a fetched state is used before it counts as stale, in minutes
cache_ttl_minutes = 5
# How cards and the footer draw their icons
#   "nerd"  : Nerd Font glyphs (the default; needs a Nerd Font in the terminal)
#   "ascii" : ASCII stand-ins
#   "none"  : no symbols — states are spelled out as words
icons = "nerd"
# Draw link rows as OSC 8 hyperlinks. A terminal that supports them opens the browser on click;
# one that does not shows plain text
hyperlinks = true

# The kanban columns, drawn in the order written here.
# kind = "open" | "terminal" (a terminal column folds on the board and is left out of list)
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
# GitHub Enterprise Server hosts, used to tell what kind of link a URL is
# ghes_hosts = ["github.example.com"]

# The gh account each fetch runs as, for when gh's active account is not the one that can see the
# repository. A key is "<host>" or "<host>/<owner>", resolved owner-first, then host, then gh's
# active account.
# One host carries both personal and organization repositories and no single account can read
# both, so a host-only entry leaves one of them answering 404.
# The token comes from "gh auth token --hostname <host> --user <account>" and is handed to the gh
# subprocess only — never written to the config or the cache.
# [github.accounts]
# "github.com/your-account" = "your-account"
# "github.com/some-org" = "work-account"
# "github.example.com" = "your-enterprise-account"

[jira]
# site = "your-tenant.atlassian.net"
# email = "you@example.com"
# The API token is read from this environment variable, never stored in the config
token_env = "TASKHERD_JIRA_TOKEN"
# When that variable is empty the token is read from this file instead. A leading ~/ expands to
# HOME. A board opened as a herdr plugin inherits the herdr server's environment, so a shell
# variable may not reach it; set this if you open the board that way (chmod 600 recommended)
# token_file = "~/.config/taskherd/jira_token"

[session_start]
# The prompt a session starts with when raised from a task (g on the board, or taskherd start).
# Placeholders: {{id}} {{title}} {{note}} {{status}} {{links}}
# {{links}} expands to "- <url>" rows and its whole line drops when there are no links.
# Set it to "" to start the agent without sending any prompt.
prompt_template = """
Please work on #{{id}} {{title}}.

Current status: {{status}}

{{note}}
{{links}}"""

# Override the template per column id. A column not named here uses prompt_template.
# [session_start.templates]
# "review" = "Please go over #{{id}} {{title}} and tell me what to look for in review.\n\n{{links}}"
# "todo" = ""
`
