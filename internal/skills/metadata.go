package skills

import "fmt"

// ReadMetadata reads bounded metadata from an original SKILL.md file or skill
// directory. It applies no installation or enablement policy and proves neither
// host discovery nor loading; callers must check effective host policy separately.
func ReadMetadata(path, host string) (Entry, error) {
	if host != "codex" && host != "claude" {
		return Entry{}, fmt.Errorf("unknown skill host %q", host)
	}
	return readSkill(skillPath(path), host)
}
