package roles

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PersonalPath names the skills-only personal role configuration.
func PersonalPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kitsu", "roles.toml"), nil
}

// ParsePersonal accepts only version and per-host, per-role skill lists.
func ParsePersonal(data []byte) (*Config, error) { return parseConfig(data, true) }

// LoadPersonal reads personal selections without repository policy or models.
func LoadPersonal() (*Config, error) {
	path, err := PersonalPath()
	if err != nil {
		return nil, fmt.Errorf("locate personal role configuration: %w", err)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{Skills: map[string]map[string][]string{}}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg, err := ParsePersonal(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func mergeSkills(personal, repository *Config) {
	for host, selections := range personal.Skills {
		if repository.Skills[host] == nil {
			repository.Skills[host] = map[string][]string{}
		}
		for role, refs := range selections {
			if _, exists := repository.Skills[host][role]; !exists {
				repository.Skills[host][role] = refs
			}
		}
	}
}

func (p *parser) skillList(host, role string, value any, line int) {
	list, ok := value.([]any)
	if !ok {
		p.fail(line, "`skills` under [%s.%s] must be an array of references", host, role)
		return
	}
	refs, seen := []string{}, map[string]bool{}
	for _, item := range list {
		ref, ok := item.(string)
		if !ok || !validSkillRef(ref) || seen[ref] {
			p.fail(line, "`skills` under [%s.%s] has an invalid or duplicate reference %v", host, role, item)
			continue
		}
		seen[ref], refs = true, append(refs, ref)
	}
	p.cfg.Skills[host][role] = refs
}

func validSkillRef(ref string) bool {
	return ref != "" && ref == strings.TrimSpace(ref) && utf8.ValidString(ref) && strings.IndexFunc(ref, unicode.IsControl) < 0
}
