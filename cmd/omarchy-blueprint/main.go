package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Grenco/omarchy-blueprint/internal/app"
)

func main() {
	// Commands run detached from the terminal, so an interrupt reaches them
	// by cancelling this context. A second interrupt exits immediately.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	os.Exit(app.Execute(ctx, os.Args[1:], app.Dependencies{}))
}
