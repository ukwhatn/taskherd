package herdrc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/ukwhatn/taskherd/internal/i18n"
)

// requestTimeout bounds a single request/response exchange. Subscriptions are not bounded by it.
const requestTimeout = 5 * time.Second

// APIError is an error returned by herdr itself, e.g. {"code":"agent_not_found"}.
// Callers branch on Code: some codes are expected outcomes rather than failures.
type APIError struct {
	Code    string
	Message string
}

func (e *APIError) Error() string {
	text, _ := e.Localize(i18n.For(i18n.LangEN))
	return text
}

// Localize names the code herdr returned, and its message when there is one. The code is left
// untranslated: it is herdr's identifier, and the thing worth searching for.
func (e *APIError) Localize(t *i18n.Catalog) (string, string) {
	herd := i18n.OrDefault(t).Err.Herd
	if e.Message == "" {
		return fmt.Sprintf(herd.APICode, e.Code), ""
	}
	return fmt.Sprintf(herd.APIMessage, e.Code, e.Message), ""
}

// UnavailableError reports that herdr could not be reached at all, which is the signal
// to fall back to the degraded mode where the herdr-backed features are switched off.
type UnavailableError struct {
	SocketPath string
	Err        error
}

func (e *UnavailableError) Error() string {
	text, _ := e.Localize(i18n.For(i18n.LangEN))
	return text
}

func (e *UnavailableError) Unwrap() error { return e.Err }

// Localize states the failure and what still works without herdr.
func (e *UnavailableError) Localize(t *i18n.Catalog) (string, string) {
	entry := i18n.OrDefault(t).Err.Herd.Unavailable
	return fmt.Sprintf(entry.Msg, e.SocketPath, e.Err), entry.Hint
}

// envelope is one line of the NDJSON protocol, in either direction.
type envelope struct {
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params any             `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// call performs one request/response exchange on its own connection.
//
// herdr closes the connection once a non-subscription request is answered, so a connection is
// never reused: a second request on the same connection fails with a broken pipe.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, &UnavailableError{SocketPath: c.socketPath, Err: err}
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(requestTimeout))
	}

	if err := writeRequest(conn, method, params); err != nil {
		return nil, err
	}
	return readResponse(bufio.NewReader(conn))
}

func writeRequest(conn net.Conn, method string, params any) error {
	if params == nil {
		params = struct{}{}
	}
	data, err := json.Marshal(envelope{ID: "taskherd:" + method, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("cannot build the request: %w", err)
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("cannot send %s: %w", method, err)
	}
	return nil
}

func readResponse(reader *bufio.Reader) (json.RawMessage, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, fmt.Errorf("cannot read the response: %w", err)
	}

	var env envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return nil, fmt.Errorf("cannot parse the response: %w", err)
	}
	if env.Error != nil {
		return nil, &APIError{Code: env.Error.Code, Message: env.Error.Message}
	}
	return env.Result, nil
}

// Snapshot fetches the current herdr session state.
func (c *Client) Snapshot(ctx context.Context) (*Snapshot, error) {
	result, err := c.call(ctx, "session.snapshot", struct{}{})
	if err == nil {
		return decodeSnapshot(result)
	}

	// The CLI resolves the socket by herdr's own rules, so it can still succeed where the
	// derived path does not (custom config, named session).
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) {
		return nil, err
	}
	snapshot, cliErr := c.snapshotViaCLI(ctx)
	if cliErr != nil {
		return nil, err
	}
	return snapshot, nil
}
