package fixture

import "net/http"

type Principal struct{ Owner string }
type Store interface {
	Delete(owner, object string) error
}

func DeleteObject(w http.ResponseWriter, r *http.Request, verified Principal, store Store) {
	owner := r.PathValue("owner")
	object := r.PathValue("object")
	if verified.Owner == "" {
		http.Error(w, "unauthenticated", 401)
		return
	}
	if err := store.Delete(owner, object); err != nil {
		http.Error(w, "failed", 500)
		return
	}
	w.WriteHeader(204)
}
