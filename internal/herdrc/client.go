// Package herdrc talks to herdr: session snapshots and live event subscriptions over the
// raw socket, and pane/tab/agent operations through the herdr CLI.
//
// Every herdr-backed feature is additive. When herdr cannot be reached the caller keeps
// working without it, so all entry points report unreachability as a value rather than
// aborting the command.
package herdrc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Source identifies taskherd as the writer of pane metadata. herdr limits it to 80 ASCII
// characters from [A-Za-z0-9:._-].
const Source = "plugin:taskherd"

// taskTokenTTL is the maximum TTL herdr accepts for reported metadata (24h).
const taskTokenTTL = 86400000

// Dialer opens a connection to the herdr socket. Tests substitute their own.
type Dialer func(ctx context.Context, socketPath string) (net.Conn, error)

// Runner runs the herdr CLI and returns its stdout. Tests substitute their own.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

// Options configures a Client. Only Getenv is required; the rest default to the real herdr.
type Options struct {
	Getenv func(string) string
	Dialer Dialer
	Runner Runner
}

// Client is a handle on one herdr server.
type Client struct {
	socketPath string
	paneID     string
	inHerdr    bool
	dialer     Dialer
	runner     Runner
}

// New builds a Client from the environment: HERDR_SOCKET_PATH, HERDR_SESSION and
// HERDR_PANE_ID for the socket and the current pane, HERDR_BIN_PATH for the CLI.
func New(opts Options) *Client {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	dialer := opts.Dialer
	if dialer == nil {
		dialer = dialUnix
	}
	runner := opts.Runner
	if runner == nil {
		runner = &execRunner{bin: resolveBin(getenv)}
	}

	return &Client{
		socketPath: ResolveSocketPath(getenv),
		paneID:     getenv("HERDR_PANE_ID"),
		inHerdr:    getenv("HERDR_ENV") == "1",
		dialer:     dialer,
		runner:     runner,
	}
}

// ResolveSocketPath applies herdr's own resolution order so that a named session is not
// mistaken for the default one: HERDR_SOCKET_PATH > HERDR_SESSION > the default socket.
func ResolveSocketPath(getenv func(string) string) string {
	if path := getenv("HERDR_SOCKET_PATH"); path != "" {
		return path
	}
	base := configDir(getenv)
	if session := getenv("HERDR_SESSION"); session != "" {
		return filepath.Join(base, "sessions", session, "herdr.sock")
	}
	return filepath.Join(base, "herdr.sock")
}

func configDir(getenv func(string) string) string {
	if xdg := getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "herdr")
	}
	return filepath.Join(getenv("HOME"), ".config", "herdr")
}

func resolveBin(getenv func(string) string) string {
	if bin := getenv("HERDR_BIN_PATH"); bin != "" {
		return bin
	}
	return "herdr"
}

// SocketPath returns the socket this client talks to.
func (c *Client) SocketPath() string { return c.socketPath }

// CurrentPaneID returns HERDR_PANE_ID, empty outside a herdr-managed pane.
func (c *Client) CurrentPaneID() string { return c.paneID }

// InHerdr reports whether this process runs inside a herdr-managed pane.
func (c *Client) InHerdr() bool { return c.inHerdr }

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	return c.dialer(ctx, c.socketPath)
}

func dialUnix(ctx context.Context, socketPath string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", socketPath)
}

type execRunner struct {
	bin string
}

func (r *execRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.bin, args...)
	out, err := cmd.Output()
	if err != nil {
		// herdr reports failures as a JSON error envelope on stdout; prefer that over the exit status.
		if apiErr := parseCLIError(out); apiErr != nil {
			return out, apiErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return out, fmt.Errorf("%s %v: %s", r.bin, args, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return out, fmt.Errorf("%s %v の実行に失敗した: %w", r.bin, args, err)
	}
	if apiErr := parseCLIError(out); apiErr != nil {
		return out, apiErr
	}
	return out, nil
}

// snapshotViaCLI is the fallback path used when the derived socket path does not resolve.
func (c *Client) snapshotViaCLI(ctx context.Context) (*Snapshot, error) {
	callCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	out, err := c.runner.Run(callCtx, "api", "snapshot")
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("herdr api snapshot の出力を解析できない: %w", err)
	}
	if env.Error != nil {
		return nil, &APIError{Code: env.Error.Code, Message: env.Error.Message}
	}
	return decodeSnapshot(env.Result)
}

// Status is what the CLI needs in order to render the degraded-mode notice once.
type Status struct {
	Available bool
	Err       error
}

// Probe fetches a snapshot and reports availability instead of failing, so that a command
// can show live state when herdr answers and carry on when it does not.
func (c *Client) Probe(ctx context.Context) (*Snapshot, Status) {
	snapshot, err := c.Snapshot(ctx)
	if err != nil {
		return nil, Status{Err: err}
	}
	return snapshot, Status{Available: true}
}

// requestContext bounds one CLI or socket operation when the caller gave no deadline.
func requestContext(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}
