package runner

import (
	"strings"
	"testing"
)

// BatchRefusal decides from the name of the file and the arguments alone, so
// its rule is tested on every system.
func TestBatchRefusal(t *testing.T) {
	t.Parallel()
	plain := []string{"--name=value", "a.b/c:d", "x+y,z@w", `back\slash-1_2`, "ABCxyz019"}
	for _, program := range []string{`C:\tools\check.bat`, `C:\tools\check.cmd`, `C:\tools\CHECK.BAT`, `C:\tools\Check.Cmd`} {
		if reason := BatchRefusal(program, plain); reason != "" {
			t.Errorf("%s with plain arguments is refused: %s", program, reason)
		}
		if reason := BatchRefusal(program, nil); reason != "" {
			t.Errorf("%s without arguments is refused: %s", program, reason)
		}
		for _, arg := range []string{"a b", "c&d", "", "100%", "e^f", `"q"`, "a|b", "a<b", "a>b", "(x)", "a;b", "a!b", "\u00fc", "a\tb", "a\nb", "a*b", "a?b", "a'b", "~a", "a#b", "a$b", "{a}", "[a]"} {
			reason := BatchRefusal(program, append([]string{"plain"}, arg))
			if !strings.Contains(reason, "is a batch file") || !strings.Contains(reason, "`"+arg+"`") {
				t.Errorf("%s with the argument %q: the reason %q does not refuse it by name", program, arg, reason)
			}
			if strings.Contains(reason, "`plain`") {
				t.Errorf("%s with the argument %q: the reason %q names the plain argument too", program, arg, reason)
			}
		}
	}
	for _, program := range []string{`C:\tools\check.exe`, `C:\tools\check`, `/usr/bin/check`, `C:\tools\check.bat.exe`} {
		if reason := BatchRefusal(program, []string{"a b", "c&d", "", "100%"}); reason != "" {
			t.Errorf("%s is no batch file and is refused: %s", program, reason)
		}
	}
}
