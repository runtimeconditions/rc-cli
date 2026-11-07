package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/runtimeconditions/rc-cli/internal/cli"
)

// Release builds set these with -ldflags -X main.version=... -X main.commit=....
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Execute(ctx, os.Args[1:], cli.BuildInfo{
		Version: version,
		Commit:  commit,
	}, cli.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
}
