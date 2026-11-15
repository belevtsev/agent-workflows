package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func selectedSkills(config *roles.Config, host, role string) []string {
	if config != nil && config.Skills[host][role] != nil {
		return config.Skills[host][role]
	}
	return []string{}
}

func roleSkills(config *roles.Config, role string) map[string][]string {
	return map[string][]string{"claude": selectedSkills(config, "claude", role), "codex": selectedSkills(config, "codex", role)}
}

func skillListText(refs []string) string {
	if len(refs) == 0 {
		return "none pinned"
	}
	return strings.Join(refs, ", ")
}

func printRoleSkillLists(cfg Config, config *roles.Config, role, label string) {
	for _, host := range roles.Hosts {
		writeRoleLine(cfg.Out, fmt.Sprintf("%s (%s): %s.", label, host, skillListText(selectedSkills(config, host, role))))
	}
}

// writeRoleLine keeps role displays within a terminal, including long IDs.
func writeRoleLine(w io.Writer, text string) {
	for line := range strings.SplitSeq(text, "\n") {
		runes := []rune(line)
		for len(runes) > 120 {
			at := 120
			for i := at; i > 4; i-- {
				if runes[i] == ' ' {
					at = i
					break
				}
			}
			fmt.Fprintln(w, string(runes[:at]))
			runes = []rune("    " + strings.TrimSpace(string(runes[at:])))
		}
		fmt.Fprintln(w, string(runes))
	}
}
