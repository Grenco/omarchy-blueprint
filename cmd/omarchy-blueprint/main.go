package main

import (
	"context"
	"os"

	"github.com/Grenco/omarchy-blueprint/internal/app"
)

func main() { os.Exit(app.Execute(context.Background(), os.Args[1:], app.Dependencies{})) }
