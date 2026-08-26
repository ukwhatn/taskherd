package main

import (
	"fmt"
	"os"
	"time"

	"github.com/ukwhatn/taskherd/internal/cli"
	"github.com/ukwhatn/taskherd/internal/config"
	"github.com/ukwhatn/taskherd/internal/i18n"
)

func main() {
	paths, err := config.ResolvePaths(os.Getenv)
	if err != nil {
		// Only the environment can be consulted for the language here: the failure is that there
		// is nowhere to look for config.toml.
		text := i18n.For(i18n.Resolve(os.Getenv, ""))
		msg, hint := i18n.Message(text, err)
		fmt.Fprintf(os.Stderr, text.CLI.Root.ErrorPrefix, msg)
		if hint != "" {
			fmt.Fprintf(os.Stderr, text.CLI.Root.HintPrefix, hint)
		}
		os.Exit(1)
	}

	os.Exit(cli.Run(cli.Env{
		Paths:  paths,
		Out:    os.Stdout,
		Err:    os.Stderr,
		In:     os.Stdin,
		Now:    time.Now,
		Getenv: os.Getenv,
	}, os.Args[1:]))
}
