package cli

import (
	"errors"
	"flag"
	"io"
	"strings"
)

// parsed is a command line after its flags were read.
type parsed struct {
	pos  []string // arguments that are not flags, in order
	rest []string // everything after "--", untouched
	dash bool     // the command line held "--"
	help bool     // -h or --help was given
}

// stringList is a flag that may be given more than once.
type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

// newFlags is a flag set that prints nothing itself: the command reports.
func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// parseArgs reads flags wherever they stand before "--", so that
// "cw run lint --force" and "cw run --force lint" mean the same. What
// follows "--" is never read as a flag.
func parseArgs(fs *flag.FlagSet, args []string) (parsed, error) {
	var p parsed
	for i, a := range args {
		if a == "--" {
			p.dash = true
			p.rest = args[i+1:]
			args = args[:i]
			break
		}
	}
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				p.help = true
				return p, nil
			}
			return p, usagef("%s", strings.TrimPrefix(err.Error(), "flag "))
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		p.pos = append(p.pos, args[0])
		args = args[1:]
	}
	return p, nil
}
