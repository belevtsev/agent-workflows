package teams

// Role is a member's role in a team.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// Team is a named group of members keyed by email address.
type Team struct {
	Name    string
	Members map[string]Role
}

// IsAdmin reports whether email is an admin of the team.
func (t Team) IsAdmin(email string) bool {
	return t.Members[email] == RoleAdmin
}
