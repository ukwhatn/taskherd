package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/model"
)

type noteMode int

const (
	noteModeSet noteMode = iota
	noteModeAppend
)

func (a *app) noteCmd() *cobra.Command {
	var (
		setText    string
		appendText string
	)

	cmd := &cobra.Command{
		Use:   "note <id>",
		Short: "note を編集する（既定は $EDITOR で開く）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			changedSet := cmd.Flags().Changed("set")
			changedAppend := cmd.Flags().Changed("append")
			if changedSet && changedAppend {
				return &UserError{
					Msg:      "--set と --append は同時に指定できない",
					HintText: "上書きは --set、追記は --append のどちらか一方を指定する",
				}
			}

			mode, text := noteModeSet, setText
			switch {
			case changedSet:
			case changedAppend:
				mode, text = noteModeAppend, appendText
			default:
				edited, err := a.editNoteInEditor(id)
				if err != nil {
					return err
				}
				text = edited
			}

			now := a.env.Now()
			var updated *model.Task
			err = a.tasks().Update(cmd.Context(), func(f *model.File) error {
				task, err := f.Task(id)
				if err != nil {
					return err
				}
				if mode == noteModeAppend {
					task.AppendNote(text, now)
				} else {
					task.SetNote(text, now)
				}
				updated = task
				return nil
			})
			if err != nil {
				return err
			}
			return a.emitTask(updated, fmt.Sprintf("#%d の note を更新した", updated.ID))
		},
	}

	cmd.Flags().StringVar(&setText, "set", "", "note を指定文字列で上書きする")
	cmd.Flags().StringVar(&appendText, "append", "", "note に追記する")
	return cmd
}

// editNoteInEditor opens the current note in $EDITOR and returns the edited text.
// The lock is not held while the editor runs, so only the note field is written back
// afterwards; concurrent changes to other fields and other tasks survive.
func (a *app) editNoteInEditor(id int) (string, error) {
	if a.jsonOut {
		return "", &UserError{
			Msg:      "note の編集内容が指定されていない",
			HintText: "--set か --append を指定する（--json では $EDITOR を起動しない）",
		}
	}

	editor := a.env.Getenv("VISUAL")
	if editor == "" {
		editor = a.env.Getenv("EDITOR")
	}
	if editor == "" {
		return "", &UserError{
			Msg:      "$EDITOR が設定されていない",
			HintText: "EDITOR を設定するか、--set / --append で非対話に指定する",
		}
	}

	f, err := a.tasks().Load()
	if err != nil {
		return "", err
	}
	task, err := f.Task(id)
	if err != nil {
		return "", err
	}

	tmpName, err := writeTempNote(id, task.Note)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = os.Remove(tmpName)
	}()

	argv := strings.Fields(editor)
	// The editor needs the real terminal, so Env's writers are bypassed here.
	command := exec.Command(argv[0], append(argv[1:], tmpName)...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("%s の起動に失敗した: %w", editor, err)
	}

	data, err := os.ReadFile(tmpName)
	if err != nil {
		return "", fmt.Errorf("編集結果を読めない: %w", err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}

func writeTempNote(id int, note string) (string, error) {
	tmp, err := os.CreateTemp("", fmt.Sprintf("taskherd-note-%d-*.md", id))
	if err != nil {
		return "", fmt.Errorf("一時ファイルを作れない: %w", err)
	}
	if _, err := tmp.WriteString(note); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("一時ファイルに書けない: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("一時ファイルを閉じられない: %w", err)
	}
	return tmp.Name(), nil
}

func (a *app) confirm(prompt string) (bool, error) {
	fmt.Fprintf(a.env.Out, "%s。よろしいか [y/N]: ", prompt)

	line, err := bufio.NewReader(a.env.In).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("入力を読めない: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
