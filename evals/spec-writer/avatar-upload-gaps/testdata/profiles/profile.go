package profiles

import (
	"encoding/json"
	"net/http"
)

// Profile is what GET /profile returns for the signed-in user.
type Profile struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// initialsAvatar is the generated avatar every profile starts with.
func initialsAvatar(name string) string {
	return "/avatars/initials/" + name + ".svg"
}

// Handler serves GET /profile.
type Handler struct {
	Lookup func(user string) (Profile, bool)
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := h.Lookup(r.Header.Get("X-User"))
	if !ok {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	if p.AvatarURL == "" {
		p.AvatarURL = initialsAvatar(p.Name)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(p)
}
