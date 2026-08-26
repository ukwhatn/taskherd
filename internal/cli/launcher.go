package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ukwhatn/taskherd/internal/i18n"
)

// detachedLogName is the file under the state directory that a detached launch's output goes to.
//
// It has to go somewhere that is not the pane: the board is a herdr overlay and its pty is gone
// moments after the launch starts, so an inherited stdout would start failing mid-run. Nothing
// reads this file automatically — it is what a failure can be looked up in after the fact, next
// to the herdr notification that announced it.
const detachedLogName = "detached.log"

// detachedLauncher runs the pane-creating half of the board's work as a process of its own.
//
// The board cannot be that process. Getting from "start this task" to "session linked, prompt
// delivered" takes around half a minute — herdr's readiness wait, then claude's integration hook
// reporting a session id — and the board is an overlay the user closes the moment the new tab
// appears. Everything the board used to do in-process past `agent start` was simply lost when
// that happened. So the board hands the whole sequence to taskherd's own CLI, detached from the
// pane's session, and quits.
type detachedLauncher struct {
	exePath  string
	stateDir string
	now      func() time.Time
	// text names the operation in the --notify-error label the child raises its failure under.
	// Resolved here rather than in the child so that the notification reads in the same language
	// as the board that asked for the launch.
	text *i18n.Catalog
}

// newDetachedLauncher resolves the binary to re-exec. It fails only when the running executable
// cannot be located at all, in which case the board runs without a launcher and says so rather
// than guessing at a name on PATH.
func newDetachedLauncher(stateDir string, now func() time.Time, text *i18n.Catalog) (*detachedLauncher, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve taskherd's own path: %w", err)
	}
	return &detachedLauncher{exePath: exe, stateDir: stateDir, now: now, text: i18n.OrDefault(text)}, nil
}

func (l *detachedLauncher) StartSession(taskID int, cwd, prompt string) error {
	return l.spawn(startSessionArgs(l.text, taskID, cwd, prompt))
}

func (l *detachedLauncher) ResumeSession(taskID int, sessionID string) error {
	return l.spawn(resumeSessionArgs(l.text, taskID, sessionID))
}

// startSessionArgs is the argv of a detached `taskherd start`.
//
// --prompt is always passed, empty included: an omitted flag means "fall back to the config
// template", and the modal's prompt field is exactly what the user decided to send, blank or not.
func startSessionArgs(text *i18n.Catalog, taskID int, cwd, prompt string) []string {
	text = i18n.OrDefault(text)
	return []string{
		"start", strconv.Itoa(taskID),
		"--cwd", cwd,
		"--prompt", prompt,
		"--notify-error", fmt.Sprintf(text.CLI.Start.LaunchLabel, taskID),
	}
}

// resumeSessionArgs is the argv of a detached `taskherd jump` for a session whose pane is gone.
// --yes is safe here because the board already asked: runConfirm only reaches resumeCmd on y.
func resumeSessionArgs(text *i18n.Catalog, taskID int, sessionID string) []string {
	text = i18n.OrDefault(text)
	return []string{
		"jump", strconv.Itoa(taskID),
		"--session", sessionID,
		"--yes",
		"--notify-error", fmt.Sprintf(text.CLI.Start.ResumeLabel, taskID),
	}
}

// spawn starts the child and returns as soon as it exists, without waiting for it.
//
// Setsid is what makes this survive: it puts the child in a session of its own, so the SIGHUP
// herdr sends when the board's pane goes away never reaches it. The working directory is
// deliberately inherited — TASKHERD_CONFIG may hold a relative path, and moving the child would
// change which config it reads.
func (l *detachedLauncher) spawn(args []string) error {
	log, err := l.openLog()
	if err != nil {
		return err
	}
	defer func() {
		_ = log.Close()
	}()

	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = strconv.Quote(arg)
	}
	// One line per launch, with every argument quoted: the prompt is multi-line, and an unquoted
	// one would make the log unreadable as a record of separate runs.
	fmt.Fprintf(log, "=== %s taskherd %s\n", l.stamp().Format(time.RFC3339), strings.Join(quoted, " "))

	cmd := exec.Command(l.exePath, args...)
	cmd.Stdin = nil
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot spawn taskherd: %w", err)
	}
	// Released rather than waited on: this process is about to exit, and the child is meant to
	// outlive it.
	return cmd.Process.Release()
}

func (l *detachedLauncher) openLog() (*os.File, error) {
	if err := os.MkdirAll(l.stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot create %s: %w", l.stateDir, err)
	}
	path := filepath.Join(l.stateDir, detachedLogName)
	log, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", path, err)
	}
	return log, nil
}

func (l *detachedLauncher) stamp() time.Time {
	if l.now == nil {
		return time.Now()
	}
	return l.now()
}
