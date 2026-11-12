//go:build !darwin && !linux

package usagelog

import "os"

func openHistory(string, bool) (*os.File, bool, error) { return nil, false, errHistoryPlatform }
