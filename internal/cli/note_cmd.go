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
		Short: a.text.CLI.Note.Short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := a.parseID(args[0])
			if err != nil {
				return err
			}
			changedSet := cmd.Flags().Changed("set")
			changedAppend := cmd.Flags().Changed("append")
			if changedSet && changedAppend {
				return &UserError{
					Msg:      a.text.CLI.Note.BothSetAndAppend.Msg,
					HintText: a.text.CLI.Note.BothSetAndAppend.Hint,
				}
			}

			var (
				mode noteMode
				text string
			)
			switch {
			case changedSet:
				mode, text = noteModeSet, setText
			case changedAppend:
				mode, text = noteModeAppend, appendText
			default:
				edited, err := a.editNoteInEditor(id)
				if err != nil {
					return err
				}
				mode, text = noteModeSet, edited
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
			return a.emitTask(updated, fmt.Sprintf(a.text.CLI.Note.Updated, updated.ID))
		},
	}

	cmd.Flags().StringVar(&setText, "set", "", a.text.CLI.Note.FlagSet)
	cmd.Flags().StringVar(&appendText, "append", "", a.text.CLI.Note.FlagAppend)
	return cmd
}

// editNoteInEditor opens the current note in $EDITOR and returns the edited text.
// The lock is not held while the editor runs, so only the note field is written back
// afterwards; concurrent changes to other fields and other tasks survive.
func (a *app) editNoteInEditor(id int) (string, error) {
	if a.jsonOut {
		return "", &UserError{
			Msg:      a.text.CLI.Note.NothingToSet.Msg,
			HintText: a.text.CLI.Note.NothingToSet.Hint,
		}
	}

	cfg, err := a.config()
	if err != nil {
		return "", err
	}
	editor := cfg.ResolveEditor(a.env.Getenv)
	if editor == "" {
		return "", &UserError{
			Msg:      a.text.CLI.Note.NoEditor.Msg,
			HintText: a.text.CLI.Note.NoEditor.Hint,
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
		return "", fmt.Errorf(a.text.CLI.Note.EditorFailed, editor, err)
	}

	data, err := os.ReadFile(tmpName)
	if err != nil {
		return "", fmt.Errorf("cannot read the edited note: %w", err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}

func writeTempNote(id int, note string) (string, error) {
	tmp, err := os.CreateTemp("", fmt.Sprintf("taskherd-note-%d-*.md", id))
	if err != nil {
		return "", fmt.Errorf("cannot create a temp file: %w", err)
	}
	if _, err := tmp.WriteString(note); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("cannot write the temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("cannot close the temp file: %w", err)
	}
	return tmp.Name(), nil
}

func (a *app) confirm(prompt string) (bool, error) {
	fmt.Fprintf(a.env.Out, a.text.CLI.Root.ConfirmPrompt, prompt)

	line, err := a.readInput()
	if err != nil {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// readLine prompts for one line of input.
func (a *app) readLine(prompt string) (string, error) {
	fmt.Fprintf(a.env.Out, "%s: ", prompt)
	line, err := a.readInput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// readInput reads through one buffered reader kept for the whole invocation, so a second
// prompt does not lose input the first one buffered.
func (a *app) readInput() (string, error) {
	if a.stdin == nil {
		a.stdin = bufio.NewReader(a.env.In)
	}
	line, err := a.stdin.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("cannot read stdin: %w", err)
	}
	return line, nil
}
