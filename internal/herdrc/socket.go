package herdrc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
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
	if e.Message == "" {
		return fmt.Sprintf("herdr エラー (%s)", e.Code)
	}
	return fmt.Sprintf("herdr エラー (%s): %s", e.Code, e.Message)
}

// UnavailableError reports that herdr could not be reached at all, which is the signal
// to fall back to the degraded mode where the herdr-backed features are switched off.
type UnavailableError struct {
	SocketPath string
	Err        error
}

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("herdr に接続できない (%s): %v", e.SocketPath, e.Err)
}

func (e *UnavailableError) Unwrap() error { return e.Err }

// Hint implements the hinter interface consumed by the CLI error reporter.
func (e *UnavailableError) Hint() string {
	return "herdr が起動していない可能性がある。herdr 連携機能（セッション状態・jump・--session current）以外は herdr なしで動作する"
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
		return fmt.Errorf("リクエストを生成できない: %w", err)
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("%s を送信できない: %w", method, err)
	}
	return nil
}

func readResponse(reader *bufio.Reader) (json.RawMessage, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, fmt.Errorf("応答を読めない: %w", err)
	}

	var env envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return nil, fmt.Errorf("応答を解析できない: %w", err)
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
