package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/ukwhatn/taskherd/internal/config"
)

func (a *app) configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "設定ファイルとデータファイルを扱う",
	}
	cmd.AddCommand(a.configPathCmd(), a.configInitCmd())
	return cmd
}

func (a *app) configPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "config・データファイルのパスを表示する",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st := a.tasks()
			cache := a.cache()
			paths := struct {
				Config    string `json:"config"`
				StateDir  string `json:"state_dir"`
				Tasks     string `json:"tasks"`
				Backup    string `json:"backup"`
				Lock      string `json:"lock"`
				Cache     string `json:"cache"`
				CacheLock string `json:"cache_lock"`
			}{
				Config:    a.env.Paths.ConfigPath,
				StateDir:  st.Dir(),
				Tasks:     st.TasksPath(),
				Backup:    st.BakPath(),
				Lock:      st.LockPath(),
				Cache:     cache.Path(),
				CacheLock: cache.LockPath(),
			}

			if a.jsonOut {
				return a.emitJSON(paths)
			}
			fmt.Fprintf(a.env.Out, "config:     %s\n", paths.Config)
			fmt.Fprintf(a.env.Out, "state:      %s\n", paths.StateDir)
			fmt.Fprintf(a.env.Out, "tasks:      %s\n", paths.Tasks)
			fmt.Fprintf(a.env.Out, "backup:     %s\n", paths.Backup)
			fmt.Fprintf(a.env.Out, "lock:       %s\n", paths.Lock)
			fmt.Fprintf(a.env.Out, "cache:      %s\n", paths.Cache)
			fmt.Fprintf(a.env.Out, "cache_lock: %s\n", paths.CacheLock)
			return nil
		},
	}
}

func (a *app) configInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "既定の config.toml を生成する",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := a.env.Paths.ConfigPath
			if _, err := os.Stat(path); err == nil {
				return &UserError{
					Msg:      fmt.Sprintf("%s は既に存在する", path),
					HintText: "既存の設定を残すため上書きしない。作り直す場合は退避してから再実行する",
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s を確認できない: %w", path, err)
			}

			dir := filepath.Dir(path)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return fmt.Errorf("%s を作成できない: %w", dir, err)
			}
			if err := os.Chmod(dir, 0o700); err != nil {
				return fmt.Errorf("%s の権限を設定できない: %w", dir, err)
			}
			if err := os.WriteFile(path, []byte(config.DefaultFileContent()), 0o600); err != nil {
				return fmt.Errorf("%s を書けない: %w", path, err)
			}

			if a.jsonOut {
				return a.emitJSON(struct {
					Created string `json:"created"`
				}{Created: path})
			}
			fmt.Fprintf(a.env.Out, "%s を作成した\n", path)
			return nil
		},
	}
}
