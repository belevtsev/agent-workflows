//go:build !darwin && !linux

package usagelog

import (
	"context"
	"errors"
	"os"
)

var errHistoryPlatform = errors.New("the cost history currently supports macOS and Linux")

func lockHistory(context.Context, string, bool) (func(), error) { return nil, errHistoryPlatform }

func openHistory(string, bool) (*os.File, bool, error) { return nil, false, errHistoryPlatform }
