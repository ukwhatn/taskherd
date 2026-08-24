// Command socketcheck is a manual diagnostic for the herdr socket event stream.
//
// It connects to the herdr socket, subscribes to the same events taskherd's board uses,
// and prints every pushed line. Use it to confirm that push delivery works on a given
// herdr version before trusting the live badges in the TUI:
//
//	go run ./scripts/socketcheck -duration 30s
//
// While it runs, create or close a tab in herdr to induce events.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	duration := flag.Duration("duration", 20*time.Second, "イベントを待ち受ける時間")
	pane := flag.String("pane", os.Getenv("HERDR_PANE_ID"), "pane.agent_status_changed を購読する pane_id")
	flag.Parse()

	socketPath := os.Getenv("HERDR_SOCKET_PATH")
	if socketPath == "" {
		fmt.Fprintln(os.Stderr, "HERDR_SOCKET_PATH が未設定")
		os.Exit(1)
	}

	if err := run(socketPath, *pane, *duration); err != nil {
		fmt.Fprintf(os.Stderr, "失敗: %v\n", err)
		os.Exit(1)
	}
}

func run(socketPath, paneID string, duration time.Duration) error {
	fmt.Println("== [1] ping 専用接続でフレーミングを確認")
	if err := pingOnce(socketPath); err != nil {
		return err
	}

	fmt.Println("== [1b] 同一接続で 2 度目のリクエストを送れるか確認")
	reusable, err := pingTwice(socketPath)
	if err != nil {
		return err
	}
	fmt.Printf("== 接続の再利用: %v\n", reusable)

	fmt.Printf("== connect %s\n", socketPath)
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("接続できない: %w", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	fmt.Println("== [2] events.subscribe（新規接続の最初のリクエスト）")
	subs := []map[string]any{
		{"type": "pane.created"},
		{"type": "pane.closed"},
		{"type": "pane.exited"},
		{"type": "pane.agent_detected"},
		{"type": "tab.created"},
		{"type": "tab.closed"},
	}
	if paneID != "" {
		subs = append(subs, map[string]any{"type": "pane.agent_status_changed", "pane_id": paneID})
	}
	if err := send(conn, map[string]any{
		"id":     "chk_sub",
		"method": "events.subscribe",
		"params": map[string]any{"subscriptions": subs},
	}); err != nil {
		return err
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("subscribe 応答を読めない: %w", err)
	}
	fmt.Printf("<- %s", line)

	fmt.Printf("== [3] push 行を %s 待ち受ける（この間に tab を作る/閉じると誘発できる）\n", duration)
	deadline := time.Now().Add(duration)
	if err := conn.SetReadDeadline(deadline); err != nil {
		return err
	}
	count := 0
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			count++
			fmt.Printf("<- %s", line)
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				fmt.Printf("== 受信終了: push %d 行\n", count)
				return nil
			}
			return fmt.Errorf("受信中に切断された（push %d 行受信後）: %w", count, err)
		}
	}
}

func pingOnce(socketPath string) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("接続できない: %w", err)
	}
	defer conn.Close()

	if err := send(conn, map[string]any{"id": "chk_ping", "method": "ping", "params": map[string]any{}}); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return fmt.Errorf("ping 応答を読めない: %w", err)
	}
	fmt.Printf("<- %s", line)
	return nil
}

// pingTwice reports whether a second request survives on the same connection.
func pingTwice(socketPath string) (bool, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false, fmt.Errorf("接続できない: %w", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	for i := 1; i <= 2; i++ {
		req := map[string]any{"id": fmt.Sprintf("chk_reuse_%d", i), "method": "ping", "params": map[string]any{}}
		if err := send(conn, req); err != nil {
			fmt.Printf("== %d 回目の送信で失敗: %v\n", i, err)
			return false, nil
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("== %d 回目の応答で失敗: %v\n", i, err)
			return false, nil
		}
		fmt.Printf("<- %s", line)
	}
	return true, nil
}

func send(conn net.Conn, req map[string]any) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	fmt.Printf("-> %s\n", data)
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("送信できない: %w", err)
	}
	return nil
}
