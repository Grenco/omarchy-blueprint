package omarchy

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

type Info struct {
	Version string `json:"version"`
	Channel string `json:"channel"`
}

func Detect(ctx context.Context, runner command.Runner) (Info, error) {
	version, err := runner.Run(ctx, "omarchy", "version")
	if err != nil {
		return Info{}, fmt.Errorf("detect Omarchy version: %w", err)
	}
	version = strings.TrimSpace(version)
	if err := requireMajor(version, 4); err != nil {
		return Info{}, err
	}
	channel, err := runner.Run(ctx, "omarchy", "version", "channel")
	if err != nil {
		return Info{}, fmt.Errorf("detect Omarchy channel: %w", err)
	}
	return Info{Version: version, Channel: strings.TrimSpace(channel)}, nil
}

func requireMajor(version string, minimum int) error {
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	part := strings.SplitN(trimmed, ".", 2)[0]
	major, err := strconv.Atoi(part)
	if err != nil || major < minimum {
		return fmt.Errorf("Omarchy %q is unsupported; version 4 or newer is required", version)
	}
	return nil
}
