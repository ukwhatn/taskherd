package main

import (
	"fmt"
	"os"
	"time"

	"github.com/ukwhatn/taskherd/internal/cli"
	"github.com/ukwhatn/taskherd/internal/config"
)

func main() {
	paths, err := config.ResolvePaths(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
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
