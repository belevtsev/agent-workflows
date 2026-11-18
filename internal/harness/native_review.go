package harness

// NativeReview identifies the provider lifecycle observed for a saved review.
// This is local provenance. GitHub and other repository policies control merge.
type NativeReview struct {
	Host   string `json:"host"`
	Parent string `json:"parent_session"`
	Worker string `json:"worker_session"`
}
