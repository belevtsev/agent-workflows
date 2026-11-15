package roles

// ResetSkillSelection removes only a role's skill override, restoring personal
// inheritance without changing its model, reasoning, paths or other settings.
func ResetSkillSelection(data []byte, host, role string, personal bool) ([]byte, error) {
	if _, err := EditSelection(data, host, role, Model{}, nil, false, personal); err != nil {
		return nil, err
	}
	return editField(data, []string{host, role, "skills"}, nil, true)
}
