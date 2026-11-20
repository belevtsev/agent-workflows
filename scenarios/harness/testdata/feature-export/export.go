package export

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	format := flags.String("format", "text", "output format")
	if err := flags.Parse(args); err != nil {
		return err
	}
	switch *format {
	case "text":
		_, err := fmt.Fprintln(out, strings.Join(flags.Args(), "\n"))
		return err
	case "json":
		labels := append([]string{}, flags.Args()...)
		return json.NewEncoder(out).Encode(labels)
	default:
		return fmt.Errorf("unknown format %q", *format)
	}
}
