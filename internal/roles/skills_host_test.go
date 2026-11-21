package roles

import (
	"strings"
	"testing"
)

func TestSkillsReachWorkersWithoutChangingRoleBoundaries(t *testing.T) {
	t.Parallel()
	files, err := HostFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range All(nil) {
		tools := "," + strings.Join(role.Tools, ",") + ","
		if !strings.Contains(tools, ",Skill,") || strings.Contains(tools, ",Agent,") {
			t.Errorf("%s cannot load native skills or can spawn roles: %s", role.ID, tools)
		}
		if role.ID == "lookup" || role.ID == "reviewer" {
			if strings.Contains(tools, ",Write,") || strings.Contains(tools, ",Edit,") {
				t.Errorf("skills expanded %s's write tools: %s", role.ID, tools)
			}
		}
		if !strings.Contains(files[TeamRoot+"/agents/"+role.ID+".md"], "Skill") {
			t.Errorf("generated %s omits native skill access", role.ID)
		}
	}
	text, err := Text(Coordinator)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"skill resolve coordinator", "SHA256", "reading original SKILL.md", "receipts alone prove no loading", "Skills grant no extra writes", "outside workers' write-check intervals"} {
		if !strings.Contains(text, required) {
			t.Errorf("coordinator omits skill handoff requirement %q", required)
		}
	}
}
