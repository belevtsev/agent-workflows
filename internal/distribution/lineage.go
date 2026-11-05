package distribution

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"time"
)

// LegacyLineage checks every legacy active/retained-runtime base against the
// pinned target as HEAD. Outages, unknown commits, behind and diverged histories
// are refusals; none trigger local Git, source downloads, retries or builds.
func (client Client) LegacyLineage(ctx context.Context, bases []string, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	api, _, err := client.bases()
	if err != nil {
		return err
	}
	if !sourceRevision.MatchString(target) || len(bases) == 0 || len(bases) > 100 {
		return errors.New("legacy migration requires exact active and retained-runtime source revisions")
	}
	for _, base := range bases {
		if !sourceRevision.MatchString(base) {
			return errors.New("legacy migration source is not an exact known commit")
		}
	}
	seen := map[string]bool{}
	for _, base := range bases {
		if seen[base] {
			continue
		}
		seen[base] = true
		data, err := client.fetch(ctx, api+"/compare/"+base+"..."+target, 4<<20)
		if err != nil {
			return fmt.Errorf("legacy lineage admission unavailable for %s: %w", base, err)
		}
		var comparison struct {
			Status     string `json:"status"`
			BehindBy   int    `json:"behind_by"`
			AheadBy    int    `json:"ahead_by"`
			BaseCommit struct {
				SHA string `json:"sha"`
			} `json:"base_commit"`
			MergeBase struct {
				SHA string `json:"sha"`
			} `json:"merge_base_commit"`
		}
		if err := json.Unmarshal(data, &comparison); err != nil {
			return err
		}
		if comparison.BaseCommit.SHA != base || comparison.MergeBase.SHA != base || comparison.BehindBy != 0 || (comparison.Status != "ahead" && comparison.Status != "identical") || comparison.Status == "identical" && (base != target || comparison.AheadBy != 0) || comparison.Status == "ahead" && comparison.AheadBy <= 0 {
			return fmt.Errorf("legacy source %s is not identical to or an ancestor of pinned target %s", base, target)
		}
	}
	return nil
}
