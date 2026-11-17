package pilot

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

var pilotAnalysisOutput = flag.String("pilot-analysis-output", "", "explicit sanitized posthoc export directory; spends no model tokens")

const analysisBootstrapDraws = 10000

// Analysis is deliberately separate from the frozen solver executable. It
// reads immutable assignments and retained results, never invokes a solver,
// and never copies prompts, model messages, credentials, or host paths.
type analysisTokens struct {
	InputUncached      int64  `json:"input_uncached"`
	InputTotal         int64  `json:"input_total"`
	CacheRead          int64  `json:"cache_read"`
	CacheWrite         int64  `json:"cache_write"`
	Output             int64  `json:"output"`
	Total              int64  `json:"total"`
	ReasoningOutput    *int64 `json:"reasoning_output_tokens"`
	ReasoningObserved  int64  `json:"reasoning_observed_tokens"`
	ReasoningKnownUnit int    `json:"reasoning_known_units"`
	Units              int    `json:"units"`
}

func analysisTokenCounts(counts tokens.Counts, reasoning *int64) analysisTokens {
	got := analysisTokens{InputUncached: counts.Input, InputTotal: counts.Input + counts.CacheRead + counts.CacheWrite,
		CacheRead: counts.CacheRead, CacheWrite: counts.CacheWrite, Output: counts.Output, Total: counts.Sum(), Units: 1}
	if reasoning != nil {
		value := *reasoning
		got.ReasoningOutput, got.ReasoningObserved, got.ReasoningKnownUnit = &value, value, 1
	}
	return got
}

func (sum *analysisTokens) add(other analysisTokens) {
	sum.InputUncached += other.InputUncached
	sum.InputTotal += other.InputTotal
	sum.CacheRead += other.CacheRead
	sum.CacheWrite += other.CacheWrite
	sum.Output += other.Output // reasoning is a subset, never added here
	sum.Total += other.Total
	sum.ReasoningObserved += other.ReasoningObserved
	sum.ReasoningKnownUnit += other.ReasoningKnownUnit
	sum.Units += other.Units
	sum.ReasoningOutput = nil
	if sum.Units > 0 && sum.Units == sum.ReasoningKnownUnit {
		value := sum.ReasoningObserved
		sum.ReasoningOutput = &value
	}
}

type analysisThread struct {
	RunID       string         `json:"run_id"`
	TraceSHA256 string         `json:"trace_sha256"`
	Role        string         `json:"role"`
	StageSource string         `json:"stage_source"`
	Known       bool           `json:"known"`
	Models      []string       `json:"models"`
	Efforts     []string       `json:"efforts"`
	Tokens      analysisTokens `json:"observed_tokens"`
}

type analysisRun struct {
	RunID                    string           `json:"run_id"`
	CaseID                   string           `json:"case_id"`
	Repeat                   int              `json:"repeat"`
	Arm                      string           `json:"arm"`
	Order                    int              `json:"order"`
	Controller               int              `json:"controller"`
	State                    string           `json:"state"`
	ResultSHA256             string           `json:"result_sha256,omitempty"`
	ProtocolSHA256           string           `json:"protocol_sha256,omitempty"`
	Measured                 bool             `json:"measured"`
	AccountingComplete       bool             `json:"accounting_complete"`
	AccountingUnknownReasons []string         `json:"accounting_unknown_reasons"`
	FunctionalStatus         string           `json:"functional_status"`
	ProtocolStatus           string           `json:"protocol_status"`
	VerifiedSuccess          bool             `json:"functional_and_protocol_verified"`
	ModelElapsedNS           *int64           `json:"model_elapsed_ns_exploratory"`
	Tokens                   *analysisTokens  `json:"observed_tokens"`
	Threads                  []analysisThread `json:"threads"`
}

func analysisRole(info traceInfo) string {
	if info.ParentID == "" {
		return "coordinator"
	}
	if slices.Contains([]string{"lookup", "tests", "code", "review", "commit"}, info.Stage) {
		return info.Stage
	}
	return "worker-unassigned"
}

// Captured cumulative counters cannot account for an in-flight request whose
// final usage event was lost when its process group was forcibly terminated.
// This annotation changes no frozen result or canonical counter ownership.
func analysisTailUsageReasons(call *invocation, status string) []string {
	reasons := []string{}
	if status == "timed_out" || call != nil && call.Stopped {
		reasons = append(reasons, "tail_usage_unknown_after_forced_stop")
	}
	if call != nil && call.Cancelled {
		reasons = append(reasons, "tail_usage_unknown_after_cancellation")
	}
	if call != nil && call.Cleanup != "" {
		reasons = append(reasons, "tail_usage_unknown_after_cleanup_failure")
	}
	return reasons
}

func analysisObserved(selected cell, result *runResult) (analysisRun, error) {
	row := analysisRun{RunID: selected.ID, CaseID: selected.CaseID, Repeat: selected.Repeat, Arm: selected.Arm,
		Order: selected.Order, Controller: selected.Controller, State: "unclaimed", FunctionalStatus: "unknown", ProtocolStatus: "unknown", Threads: []analysisThread{}, AccountingUnknownReasons: []string{"result_unavailable"}}
	if result == nil {
		return row, nil
	}
	row.State, row.Measured, row.ProtocolStatus = result.Status, result.Measured, result.Method.Status
	if row.ProtocolStatus != "verified" && row.ProtocolStatus != "violated" {
		row.ProtocolStatus = "unknown"
	}
	var grade gradeSummary
	if len(result.Grade) > 0 {
		if err := json.Unmarshal(result.Grade, &grade); err != nil {
			return row, fmt.Errorf("invalid grade for %s: %w", selected.ID, err)
		}
		if grade.Infrastructure == "ok" && grade.Eligibility.Passed {
			row.FunctionalStatus = "failed"
			if functionalPass(result) {
				row.FunctionalStatus = "passed"
			}
		}
	}
	row.VerifiedSuccess = row.FunctionalStatus == "passed" && row.ProtocolStatus == "verified"
	if result.Execution != nil {
		value := result.Execution.ElapsedNS
		row.ModelElapsedNS = &value
	}
	if result.Measured || len(result.Usage.Threads) != 0 || result.Usage.Total != 0 {
		value := analysisTokenCounts(result.Usage.Counts, result.Usage.ReasoningOutput)
		row.Tokens = &value
		if result.Usage.Input < 0 || result.Usage.CacheRead < 0 || result.Usage.CacheWrite < 0 || result.Usage.Output < 0 || result.Usage.Total != value.Total {
			return row, fmt.Errorf("inconsistent or negative usage for %s", selected.ID)
		}
	}
	var threadSum tokens.Counts
	for _, info := range result.Usage.Threads {
		if info.Input < 0 || info.CacheRead < 0 || info.CacheWrite < 0 || info.Output < 0 || info.ReasoningOutput != nil && (*info.ReasoningOutput < 0 || *info.ReasoningOutput > info.Output) {
			return row, fmt.Errorf("invalid thread usage for %s", selected.ID)
		}
		addCounts(&threadSum, info.Counts)
		row.Threads = append(row.Threads, analysisThread{RunID: selected.ID, TraceSHA256: info.File.SHA256, Role: analysisRole(info), StageSource: info.StageSource,
			Known: info.Known && len(info.UnknownCategories) == 0, Models: slices.Clone(info.Models), Efforts: slices.Clone(info.Efforts), Tokens: analysisTokenCounts(info.Counts, info.ReasoningOutput)})
	}
	if row.Tokens != nil && row.Tokens.ReasoningOutput != nil && (*row.Tokens.ReasoningOutput < 0 || *row.Tokens.ReasoningOutput > row.Tokens.Output) {
		return row, fmt.Errorf("invalid reasoning subset for %s", selected.ID)
	}
	if result.Usage.Complete && !reflect.DeepEqual(threadSum, result.Usage.Counts) {
		return row, fmt.Errorf("thread totals differ from complete run totals for %s", selected.ID)
	}
	if result.Usage.Complete && (len(result.Usage.Unknown) > 0 || slices.ContainsFunc(row.Threads, func(thread analysisThread) bool { return !thread.Known })) {
		return row, fmt.Errorf("complete usage contains unknown thread evidence for %s", selected.ID)
	}
	if row.Tokens != nil && row.Tokens.ReasoningOutput == nil {
		for _, thread := range row.Threads {
			row.Tokens.ReasoningObserved += thread.Tokens.ReasoningObserved
		}
	}
	row.AccountingUnknownReasons = analysisTailUsageReasons(result.Execution, result.Status)
	if !result.Usage.Complete {
		row.AccountingUnknownReasons = append(row.AccountingUnknownReasons, "captured_usage_incomplete")
	}
	if len(row.Threads) == 0 {
		row.AccountingUnknownReasons = append(row.AccountingUnknownReasons, "no_captured_sessions")
	}
	if result.Measured && result.Execution == nil {
		row.AccountingUnknownReasons = append(row.AccountingUnknownReasons, "execution_metadata_missing")
	}
	row.AccountingComplete = result.Measured && len(row.AccountingUnknownReasons) == 0
	return row, nil
}

type analysisArm struct {
	Arm                   string         `json:"arm"`
	Assigned              int            `json:"assigned"`
	Results               int            `json:"results"`
	Measured              int            `json:"measured"`
	FunctionalPassed      int            `json:"functional_passed"`
	FunctionalFailed      int            `json:"functional_failed"`
	FunctionalUnknown     int            `json:"functional_unknown"`
	ProtocolVerified      int            `json:"protocol_verified"`
	ProtocolViolated      int            `json:"protocol_violated"`
	ProtocolUnknown       int            `json:"protocol_unknown"`
	VerifiedSuccess       int            `json:"functional_and_protocol_verified"`
	AccountingComplete    int            `json:"accounting_complete_runs"`
	ModelElapsedNS        int64          `json:"model_elapsed_ns_exploratory"`
	Tokens                analysisTokens `json:"all_attempts_observed_tokens"`
	PerFunctionalSuccess  *float64       `json:"observed_tokens_per_functional_success"`
	PerVerifiedSuccess    *float64       `json:"observed_tokens_per_verified_success"`
	AllAccountingComplete bool           `json:"all_assigned_accounting_complete"`
}

func analysisSummarizeArm(rows []analysisRun, arm string) analysisArm {
	got := analysisArm{Arm: arm}
	for _, row := range rows {
		if row.Arm != arm {
			continue
		}
		got.Assigned++
		if row.ResultSHA256 != "" {
			got.Results++
		}
		if row.Measured {
			got.Measured++
		}
		if row.AccountingComplete {
			got.AccountingComplete++
		}
		switch row.FunctionalStatus {
		case "passed":
			got.FunctionalPassed++
		case "failed":
			got.FunctionalFailed++
		default:
			got.FunctionalUnknown++
		}
		switch row.ProtocolStatus {
		case "verified":
			got.ProtocolVerified++
		case "violated":
			got.ProtocolViolated++
		default:
			got.ProtocolUnknown++
		}
		if row.VerifiedSuccess {
			got.VerifiedSuccess++
		}
		if row.Tokens != nil {
			got.Tokens.add(*row.Tokens)
		}
		if row.ModelElapsedNS != nil {
			got.ModelElapsedNS += *row.ModelElapsedNS
		}
	}
	got.AllAccountingComplete = got.Assigned > 0 && got.Assigned == got.AccountingComplete
	if got.FunctionalPassed > 0 {
		value := float64(got.Tokens.Total) / float64(got.FunctionalPassed)
		got.PerFunctionalSuccess = &value
	}
	if got.VerifiedSuccess > 0 {
		value := float64(got.Tokens.Total) / float64(got.VerifiedSuccess)
		got.PerVerifiedSuccess = &value
	}
	return got
}

type analysisPair struct {
	CaseID                string   `json:"case_id"`
	Repeat                int      `json:"repeat"`
	Normal                string   `json:"normal_run"`
	Compact               string   `json:"compact_run"`
	NormalFunctional      string   `json:"normal_functional"`
	CompactFunctional     string   `json:"compact_functional"`
	NormalProtocol        string   `json:"normal_protocol"`
	CompactProtocol       string   `json:"compact_protocol"`
	AccountingComparable  bool     `json:"accounting_comparable"`
	BothFunctionalSuccess bool     `json:"both_functional_success"`
	BothVerifiedSuccess   bool     `json:"both_verified_success"`
	FunctionalLoss        *bool    `json:"functional_loss"`
	NormalTokens          *int64   `json:"normal_observed_tokens"`
	CompactTokens         *int64   `json:"compact_observed_tokens"`
	TokenDelta            *int64   `json:"token_delta_compact_minus_normal"`
	TokenRatio            *float64 `json:"token_ratio_compact_over_normal"`
}

func analysisPairs(m manifest, rows []analysisRun) []analysisPair {
	byID := map[string]analysisRun{}
	for _, row := range rows {
		byID[row.RunID] = row
	}
	pairs := []analysisPair{}
	for _, task := range m.Cases {
		for repeat := 1; repeat <= m.Repeats; repeat++ {
			normal := byID[fmt.Sprintf("%s-r%d-normal", task.ID, repeat)]
			compact := byID[fmt.Sprintf("%s-r%d-compact", task.ID, repeat)]
			pair := analysisPair{CaseID: task.ID, Repeat: repeat, Normal: normal.RunID, Compact: compact.RunID,
				NormalFunctional: normal.FunctionalStatus, CompactFunctional: compact.FunctionalStatus, NormalProtocol: normal.ProtocolStatus, CompactProtocol: compact.ProtocolStatus,
				AccountingComparable:  normal.AccountingComplete && compact.AccountingComplete,
				BothFunctionalSuccess: normal.FunctionalStatus == "passed" && compact.FunctionalStatus == "passed", BothVerifiedSuccess: normal.VerifiedSuccess && compact.VerifiedSuccess}
			if (normal.FunctionalStatus == "passed" || normal.FunctionalStatus == "failed") && (compact.FunctionalStatus == "passed" || compact.FunctionalStatus == "failed") {
				loss := normal.FunctionalStatus == "passed" && compact.FunctionalStatus == "failed"
				pair.FunctionalLoss = &loss
			}
			if normal.Tokens != nil {
				value := normal.Tokens.Total
				pair.NormalTokens = &value
			}
			if compact.Tokens != nil {
				value := compact.Tokens.Total
				pair.CompactTokens = &value
			}
			if pair.AccountingComparable && pair.NormalTokens != nil && pair.CompactTokens != nil {
				delta := *pair.CompactTokens - *pair.NormalTokens
				pair.TokenDelta = &delta
				if *pair.NormalTokens > 0 {
					ratio := float64(*pair.CompactTokens) / float64(*pair.NormalTokens)
					pair.TokenRatio = &ratio
				}
			}
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

type analysisCI struct {
	Estimate   float64 `json:"estimate"`
	Lower      float64 `json:"lower"`
	Upper      float64 `json:"upper"`
	Confidence float64 `json:"confidence"`
	Clusters   int     `json:"clusters"`
	Unit       string  `json:"cluster_unit"`
	Draws      int     `json:"bootstrap_draws"`
	ValidDraws int     `json:"valid_draws"`
	Seed       int64   `json:"seed"`
}

func analysisClusterKeys(m manifest, unit string) ([]string, map[string]int) {
	keys := []string{}
	byTask := map[string]int{}
	for _, task := range m.Cases {
		key := task.ID
		if unit == "repository" {
			var provenance struct {
				Repo string `json:"repo"`
			}
			if json.Unmarshal(task.Provenance, &provenance) == nil && provenance.Repo != "" {
				key = provenance.Repo
			}
		}
		index := slices.Index(keys, key)
		if index == -1 {
			index = len(keys)
			keys = append(keys, key)
		}
		byTask[task.ID] = index
	}
	return keys, byTask
}

func analysisPercentileCI(values []float64, estimate float64, clusters int, unit string, seed int64) *analysisCI {
	slices.Sort(values)
	if len(values) == 0 {
		return nil
	}
	quantile := func(p float64) float64 {
		position := p * float64(len(values)-1)
		lo, hi := int(math.Floor(position)), int(math.Ceil(position))
		return values[lo] + (position-float64(lo))*(values[hi]-values[lo])
	}
	return &analysisCI{Estimate: estimate, Lower: quantile(.025), Upper: quantile(.975), Confidence: .95, Clusters: clusters, Unit: unit,
		Draws: analysisBootstrapDraws, ValidDraws: len(values), Seed: seed}
}

// ratio-of-sums and mean paired difference preserve the assigned pairing.
// Empty success subsets contribute no observations, but their task clusters
// remain in the resampling frame rather than becoming independent trials.
func analysisClusterCI(m manifest, pairs []analysisPair, subset, unit string) (*analysisCI, *analysisCI, int) {
	type cluster struct {
		normal, compact float64
		pairs           int
	}
	keys, byTask := analysisClusterKeys(m, unit)
	clusters := make([]cluster, len(keys))
	selected, contributing := 0, 0
	for _, task := range m.Cases {
		i := byTask[task.ID]
		for _, pair := range pairs {
			if pair.CaseID != task.ID || !pair.AccountingComparable || pair.NormalTokens == nil || pair.CompactTokens == nil || subset == "both_functional_success" && !pair.BothFunctionalSuccess || subset == "both_verified_success" && !pair.BothVerifiedSuccess {
				continue
			}
			clusters[i].normal += float64(*pair.NormalTokens)
			clusters[i].compact += float64(*pair.CompactTokens)
			clusters[i].pairs++
			selected++
		}
	}
	var total cluster
	for _, c := range clusters {
		if c.pairs > 0 {
			contributing++
		}
		total.normal += c.normal
		total.compact += c.compact
		total.pairs += c.pairs
	}
	if contributing < 2 || total.normal <= 0 || total.pairs == 0 {
		return nil, nil, selected
	}
	rng := rand.New(rand.NewSource(m.Seed))
	ratios, deltas := make([]float64, 0, analysisBootstrapDraws), make([]float64, 0, analysisBootstrapDraws)
	for draw := 0; draw < analysisBootstrapDraws; draw++ {
		var sampled cluster
		for range clusters {
			c := clusters[rng.Intn(len(clusters))]
			sampled.normal += c.normal
			sampled.compact += c.compact
			sampled.pairs += c.pairs
		}
		if sampled.normal > 0 && sampled.pairs > 0 {
			ratios = append(ratios, sampled.compact/sampled.normal)
			deltas = append(deltas, (sampled.compact-sampled.normal)/float64(sampled.pairs))
		}
	}
	return analysisPercentileCI(ratios, total.compact/total.normal, len(clusters), unit, m.Seed),
		analysisPercentileCI(deltas, (total.compact-total.normal)/float64(total.pairs), len(clusters), unit, m.Seed), selected
}

func analysisQualityCI(m manifest, pairs []analysisPair, verified bool, unit string) *analysisCI {
	if len(pairs) != len(m.Cases)*m.Repeats {
		return nil
	}
	keys, byTask := analysisClusterKeys(m, unit)
	if len(keys) < 2 {
		return nil
	}
	type cluster struct{ delta, pairs int }
	clusters := make([]cluster, len(keys))
	totalDelta := 0
	for _, pair := range pairs {
		if pair.FunctionalLoss == nil || verified && (pair.NormalProtocol == "unknown" || pair.CompactProtocol == "unknown") {
			return nil
		}
		normal, compact := pair.NormalFunctional == "passed", pair.CompactFunctional == "passed"
		if verified {
			normal, compact = normal && pair.NormalProtocol == "verified", compact && pair.CompactProtocol == "verified"
		}
		delta := 0
		if normal {
			delta--
		}
		if compact {
			delta++
		}
		index := byTask[pair.CaseID]
		clusters[index].delta += delta
		clusters[index].pairs++
		totalDelta += delta
	}
	rng := rand.New(rand.NewSource(m.Seed))
	values := make([]float64, 0, analysisBootstrapDraws)
	for range analysisBootstrapDraws {
		delta, count := 0, 0
		for range clusters {
			c := clusters[rng.Intn(len(clusters))]
			delta, count = delta+c.delta, count+c.pairs
		}
		if count > 0 {
			values = append(values, float64(delta)/float64(count))
		}
	}
	return analysisPercentileCI(values, float64(totalDelta)/float64(len(pairs)), len(clusters), unit, m.Seed)
}

type analysisSubset struct {
	Name                 string      `json:"name"`
	SelectedPairs        int         `json:"selected_pairs"`
	NormalTokens         int64       `json:"normal_tokens"`
	CompactTokens        int64       `json:"compact_tokens"`
	InferenceReady       bool        `json:"all_required_accounting_and_outcomes_assessed"`
	TokenRatio           *analysisCI `json:"task_cluster_token_ratio_ci"`
	MeanPairedTokenDelta *analysisCI `json:"task_cluster_mean_paired_token_delta_ci"`
	RepoTokenRatio       *analysisCI `json:"repository_cluster_token_ratio_ci_exploratory"`
	RepoMeanTokenDelta   *analysisCI `json:"repository_cluster_mean_paired_token_delta_ci_exploratory"`
}

func analysisSubsets(m manifest, pairs []analysisPair) []analysisSubset {
	groups := []analysisSubset{}
	for _, name := range []string{"all_pairs", "both_functional_success", "both_verified_success"} {
		got := analysisSubset{Name: name, InferenceReady: len(pairs) == len(m.Cases)*m.Repeats}
		for _, pair := range pairs {
			got.InferenceReady = got.InferenceReady && pair.AccountingComparable
			if name != "all_pairs" {
				got.InferenceReady = got.InferenceReady && pair.FunctionalLoss != nil
			}
			if name == "both_verified_success" {
				got.InferenceReady = got.InferenceReady && pair.NormalProtocol != "unknown" && pair.CompactProtocol != "unknown"
			}
			if !pair.AccountingComparable || pair.NormalTokens == nil || pair.CompactTokens == nil || name == "both_functional_success" && !pair.BothFunctionalSuccess || name == "both_verified_success" && !pair.BothVerifiedSuccess {
				continue
			}
			got.SelectedPairs++
			got.NormalTokens += *pair.NormalTokens
			got.CompactTokens += *pair.CompactTokens
		}
		if got.InferenceReady {
			got.TokenRatio, got.MeanPairedTokenDelta, _ = analysisClusterCI(m, pairs, name, "task")
			got.RepoTokenRatio, got.RepoMeanTokenDelta, _ = analysisClusterCI(m, pairs, name, "repository")
		}
		groups = append(groups, got)
	}
	return groups
}

type analysisLossBound struct {
	Tasks                   int      `json:"planned_task_clusters"`
	KnownLossTasks          int      `json:"known_loss_tasks"`
	FullyAssessedTasks      int      `json:"fully_assessed_tasks"`
	UnassessedTasks         int      `json:"unassessed_tasks_without_confirmed_loss"`
	BaselineSuccessTasks    int      `json:"tasks_with_a_normal_functional_success"`
	AssessedLossUpper95     *float64 `json:"one_sided_95pct_upper_loss_probability_assessed_tasks"`
	ConservativeLossUpper95 float64  `json:"one_sided_95pct_upper_loss_probability_unknown_tasks_count_as_loss"`
}

// The exact one-sided Clopper-Pearson upper bound solves P_p(X <= k)=alpha.
// The binomial recurrence is stable for this pilot's small task count. This
// is a task-level bound; assigned runs are not independent observations.
func analysisBinomialUpper(losses, tasks int, alpha float64) float64 {
	if tasks <= 0 || losses >= tasks {
		return 1
	}
	if losses == 0 {
		return 1 - math.Pow(alpha, 1/float64(tasks))
	}
	cdf := func(p float64) float64 {
		term, sum := math.Pow(1-p, float64(tasks)), 0.0
		for count := 0; count <= losses; count++ {
			sum += term
			term *= float64(tasks-count) / float64(count+1) * p / (1 - p)
		}
		return sum
	}
	lo, hi := 0.0, 1.0
	for range 100 {
		mid := (lo + hi) / 2
		if cdf(mid) > alpha {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

func analysisTaskLoss(m manifest, pairs []analysisPair) analysisLossBound {
	got := analysisLossBound{Tasks: len(m.Cases)}
	for _, task := range m.Cases {
		assessed, loss, baselineSuccess := 0, false, false
		for _, pair := range pairs {
			if pair.CaseID != task.ID {
				continue
			}
			baselineSuccess = baselineSuccess || pair.NormalFunctional == "passed"
			if pair.FunctionalLoss != nil {
				assessed++
				loss = loss || *pair.FunctionalLoss
			}
		}
		if baselineSuccess {
			got.BaselineSuccessTasks++
		}
		if assessed == m.Repeats {
			got.FullyAssessedTasks++
		}
		if loss {
			got.KnownLossTasks++
		} else if assessed != m.Repeats {
			got.UnassessedTasks++
		}
	}
	// A confirmed loss identifies that task's binary outcome even when another
	// repetition is ungraded. Unresolved tasks are excluded only from the
	// explicitly conditional assessed bound and counted as losses in the other.
	known := got.Tasks - got.UnassessedTasks
	if known > 0 {
		value := analysisBinomialUpper(got.KnownLossTasks, known, .05)
		got.AssessedLossUpper95 = &value
	}
	got.ConservativeLossUpper95 = analysisBinomialUpper(got.KnownLossTasks+got.UnassessedTasks, got.Tasks, .05)
	return got
}

type analysisCase struct {
	CaseID string        `json:"case_id"`
	Arms   []analysisArm `json:"arms"`
}

type analysisRoleTotal struct {
	Arm     string         `json:"arm"`
	Role    string         `json:"role"`
	Threads int            `json:"threads"`
	Known   int            `json:"known_threads"`
	Tokens  analysisTokens `json:"observed_tokens"`
}

type analysisReport struct {
	Version           int                 `json:"version"`
	ManifestSHA256    string              `json:"manifest_sha256"`
	CalibrationSHA256 string              `json:"calibration_excluded_sha256"`
	ExecutableSHA256  string              `json:"analysis_executable_sha256"`
	GoVersion         string              `json:"go_version"`
	Topology          string              `json:"topology"`
	Model             string              `json:"model"`
	Effort            string              `json:"effort"`
	Controllers       int                 `json:"controllers"`
	AllResults        bool                `json:"all_assigned_results_present"`
	Arms              []analysisArm       `json:"arms"`
	Cases             []analysisCase      `json:"cases"`
	RoleTotals        []analysisRoleTotal `json:"role_attribution"`
	Subsets           []analysisSubset    `json:"paired_token_comparisons"`
	TaskLoss          analysisLossBound   `json:"functional_task_loss"`
	FunctionalRateCI  *analysisCI         `json:"functional_pass_rate_delta_task_cluster_ci"`
	VerifiedRateCI    *analysisCI         `json:"verified_pass_rate_delta_task_cluster_ci"`
	FunctionalRepoCI  *analysisCI         `json:"functional_pass_rate_delta_repository_cluster_ci_exploratory"`
	VerifiedRepoCI    *analysisCI         `json:"verified_pass_rate_delta_repository_cluster_ci_exploratory"`
	Runs              []analysisRun       `json:"runs"`
	Pairs             []analysisPair      `json:"pairs"`
	Cautions          []string            `json:"cautions"`
}

func analysisBuild(m manifest, p plan, rows []analysisRun) analysisReport {
	pairs := analysisPairs(m, rows)
	repositories, _ := analysisClusterKeys(m, "repository")
	got := analysisReport{Version: 1, ManifestSHA256: p.ManifestSHA256, CalibrationSHA256: m.Calibration.SHA256, GoVersion: runtime.Version(), Topology: m.Topology, Model: m.Model, Effort: m.Effort,
		Controllers: m.Controllers, AllResults: len(rows) == len(p.Runs), Runs: rows, Pairs: pairs, Subsets: analysisSubsets(m, pairs), TaskLoss: analysisTaskLoss(m, pairs),
		FunctionalRateCI: analysisQualityCI(m, pairs, false, "task"), VerifiedRateCI: analysisQualityCI(m, pairs, true, "task"),
		FunctionalRepoCI: analysisQualityCI(m, pairs, false, "repository"), VerifiedRepoCI: analysisQualityCI(m, pairs, true, "repository"),
		Arms: []analysisArm{}, Cases: []analysisCase{}, RoleTotals: []analysisRoleTotal{},
		Cautions: []string{
			"All recorded attempt tokens are charged, including failed and timed-out candidates; missing accounting is unknown and observed partial totals are lower bounds.",
			"Forced stops, cancellation, and cleanup failures can lose the final usage event of an in-flight model request; known captured counters do not establish complete actual total usage for these runs.",
			"Input total includes uncached input, cache reads, and cache writes. Reasoning is included in output and is never added again. Tokens are not a monetary bill.",
			"Functional acceptance, infrastructure, and independently verified protocol compliance are distinct. Unknown outcomes or audits never count as verified successes.",
			"Success-conditioned token comparisons are descriptive and may select easier tasks; compare all-assignment outcomes and costs first.",
			fmt.Sprintf("Primary bootstrap intervals resample %d task IDs with all %d paired repetitions intact; exploratory repository-cluster sensitivity uses %d groups from recorded repository provenance, falling back to task IDs when unavailable.", len(m.Cases), m.Repeats, len(repositories)),
			"These convenience-selected public tasks are not random population samples. Cluster intervals describe observed variability under resampling assumptions, not guaranteed external coverage.",
			"The loss event is any normal-pass/compact-fail functional pair within a task. The exact one-sided binomial bound is descriptive under an independent-task Bernoulli assumption that related repository tasks may violate.",
			"Zero observed losses does not establish noninferiority or universal quality preservation; tasks without a normal success offer no observed successful baseline to preserve.",
			"Intervals are suppressed until the required paired accounting and outcomes are assessed; unresolved tasks count as losses in the conservative loss bound.",
			fmt.Sprintf("Elapsed times are exploratory because %d controllers share the host and model service; provider cache state and service contention are uncontrolled.", m.Controllers),
			"Role attribution uses causal parent metadata and recorded stage sources. Saved briefs and task names do not prove encrypted native message payloads were identical.",
			"Calibration tokens are excluded from these measured-run totals and must be reported separately. Public historical-task contamination and two Kitsu cases limit generalization.",
		}}
	for _, arm := range []string{"normal", "compact"} {
		got.Arms = append(got.Arms, analysisSummarizeArm(rows, arm))
	}
	for _, task := range m.Cases {
		caseRows := []analysisRun{}
		for _, row := range rows {
			if row.CaseID == task.ID {
				caseRows = append(caseRows, row)
			}
		}
		got.Cases = append(got.Cases, analysisCase{CaseID: task.ID, Arms: []analysisArm{analysisSummarizeArm(caseRows, "normal"), analysisSummarizeArm(caseRows, "compact")}})
	}
	for _, row := range rows {
		got.AllResults = got.AllResults && row.ResultSHA256 != ""
	}
	for _, arm := range []string{"normal", "compact"} {
		for _, role := range []string{"coordinator", "lookup", "tests", "code", "review", "commit", "worker-unassigned"} {
			total := analysisRoleTotal{Arm: arm, Role: role}
			for _, row := range rows {
				if row.Arm != arm {
					continue
				}
				for _, thread := range row.Threads {
					if thread.Role != role {
						continue
					}
					total.Threads++
					if thread.Known {
						total.Known++
					}
					total.Tokens.add(thread.Tokens)
				}
			}
			got.RoleTotals = append(got.RoleTotals, total)
		}
	}
	return got
}

func analysisLoad(output string) (manifest, plan, []analysisRun, error) {
	var m manifest
	var p plan
	if err := readJSON(filepath.Join(output, "frozen-manifest.json"), &m); err != nil {
		return m, p, nil, err
	}
	if err := readJSON(filepath.Join(output, "plan.json"), &p); err != nil {
		return m, p, nil, err
	}
	digest, err := hashFile(filepath.Join(output, "frozen-manifest.json"))
	if err != nil || digest != p.ManifestSHA256 || !reflect.DeepEqual(p, schedule(m, digest)) {
		return m, p, nil, errors.New("analysis input does not match its immutable assignment")
	}
	rows := make([]analysisRun, 0, len(p.Runs))
	for _, selected := range p.Runs {
		runDir := filepath.Join(output, "runs", selected.ID)
		path := filepath.Join(runDir, "result.json")
		var result runResult
		var source *runResult
		if err := readJSON(path, &result); err == nil {
			if result.Run != selected || result.ManifestSHA256 != digest {
				return m, p, nil, fmt.Errorf("result identity mismatch: %s", selected.ID)
			}
			if result.Method.Facts == nil {
				result.Method.Facts = map[string]any{}
			}
			if err := applyProtocolAudit(runDir, &result); err != nil {
				return m, p, nil, err
			}
			source = &result
		} else if !errors.Is(err, os.ErrNotExist) {
			return m, p, nil, err
		}
		row, err := analysisObserved(selected, source)
		if err != nil {
			return m, p, nil, err
		}
		if source != nil {
			row.ResultSHA256, err = hashFile(path)
			if err != nil {
				return m, p, nil, err
			}
			if source.Protocol != nil {
				row.ProtocolSHA256 = source.Protocol.SHA256
			}
		} else if _, err := os.Stat(filepath.Join(runDir, "running.json")); err == nil {
			row.State = "running_or_interrupted"
		} else if _, err := os.Stat(runDir); err == nil {
			row.State = "claimed_without_result"
		}
		rows = append(rows, row)
	}
	return m, p, rows, nil
}

func analysisWriteCSV(path string, header []string, rows [][]string) (returned error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { returned = errors.Join(returned, f.Close()) }()
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}
	if err := w.WriteAll(rows); err != nil {
		return err
	}
	return w.Error()
}

func analysisInt(value int64) string { return strconv.FormatInt(value, 10) }

func analysisOptionalInt(value *int64) string {
	if value == nil {
		return ""
	}
	return analysisInt(*value)
}

func analysisTokenCSV(value *analysisTokens) []string {
	if value == nil {
		return []string{"", "", "", "", "", "", "", "", "", ""}
	}
	return []string{analysisInt(value.InputUncached), analysisInt(value.InputTotal), analysisInt(value.CacheRead), analysisInt(value.CacheWrite), analysisInt(value.Output), analysisInt(value.Total),
		analysisOptionalInt(value.ReasoningOutput), analysisInt(value.ReasoningObserved), strconv.Itoa(value.ReasoningKnownUnit), strconv.Itoa(value.Units)}
}

func analysisArmCSV(arm analysisArm) []string {
	return append([]string{arm.Arm, strconv.Itoa(arm.Assigned), strconv.Itoa(arm.Results), strconv.Itoa(arm.Measured), strconv.Itoa(arm.FunctionalPassed), strconv.Itoa(arm.FunctionalFailed),
		strconv.Itoa(arm.FunctionalUnknown), strconv.Itoa(arm.ProtocolVerified), strconv.Itoa(arm.ProtocolViolated), strconv.Itoa(arm.ProtocolUnknown), strconv.Itoa(arm.VerifiedSuccess),
		strconv.Itoa(arm.AccountingComplete), strconv.FormatBool(arm.AllAccountingComplete), analysisInt(arm.ModelElapsedNS)}, analysisTokenCSV(&arm.Tokens)...)
}

func analysisExport(output string, report analysisReport) error {
	if err := os.MkdirAll(output, 0o700); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(output, "analysis.json"), report, false); err != nil {
		return err
	}
	tokenHeader := []string{"input_uncached", "input_total", "cache_read", "cache_write", "output", "total", "reasoning_output_subset", "reasoning_observed", "reasoning_known_units", "units"}
	armHeader := append([]string{"arm", "assigned", "results", "measured", "functional_passed", "functional_failed", "functional_unknown", "protocol_verified", "protocol_violated", "protocol_unknown",
		"verified_success", "accounting_complete_runs", "all_assigned_accounting_complete", "elapsed_ns_exploratory"}, tokenHeader...)
	runs, threads, pairs, arms, cases, roles := [][]string{}, [][]string{}, [][]string{}, [][]string{}, [][]string{}, [][]string{}
	for _, arm := range report.Arms {
		arms = append(arms, analysisArmCSV(arm))
	}
	for _, task := range report.Cases {
		for _, arm := range task.Arms {
			cases = append(cases, append([]string{task.CaseID}, analysisArmCSV(arm)...))
		}
	}
	for _, role := range report.RoleTotals {
		roles = append(roles, append([]string{role.Arm, role.Role, strconv.Itoa(role.Threads), strconv.Itoa(role.Known)}, analysisTokenCSV(&role.Tokens)...))
	}
	for _, row := range report.Runs {
		values := []string{row.RunID, row.CaseID, strconv.Itoa(row.Repeat), row.Arm, strconv.Itoa(row.Order), strconv.Itoa(row.Controller), row.State,
			strconv.FormatBool(row.Measured), strconv.FormatBool(row.AccountingComplete), strings.Join(row.AccountingUnknownReasons, ";"), row.FunctionalStatus, row.ProtocolStatus, strconv.FormatBool(row.VerifiedSuccess), analysisOptionalInt(row.ModelElapsedNS)}
		runs = append(runs, append(values, analysisTokenCSV(row.Tokens)...))
		for _, thread := range row.Threads {
			threads = append(threads, append([]string{row.RunID, thread.TraceSHA256, thread.Role, thread.StageSource, strconv.FormatBool(thread.Known)}, analysisTokenCSV(&thread.Tokens)...))
		}
	}
	for _, pair := range report.Pairs {
		loss, ratio := "", ""
		if pair.FunctionalLoss != nil {
			loss = strconv.FormatBool(*pair.FunctionalLoss)
		}
		if pair.TokenRatio != nil {
			ratio = strconv.FormatFloat(*pair.TokenRatio, 'g', -1, 64)
		}
		pairs = append(pairs, []string{pair.CaseID, strconv.Itoa(pair.Repeat), pair.Normal, pair.Compact, pair.NormalFunctional, pair.CompactFunctional, pair.NormalProtocol, pair.CompactProtocol,
			strconv.FormatBool(pair.AccountingComparable), strconv.FormatBool(pair.BothFunctionalSuccess), strconv.FormatBool(pair.BothVerifiedSuccess), loss, analysisOptionalInt(pair.NormalTokens), analysisOptionalInt(pair.CompactTokens), analysisOptionalInt(pair.TokenDelta), ratio})
	}
	for _, file := range []struct {
		name   string
		header []string
		rows   [][]string
	}{
		{"runs.csv", append([]string{"run_id", "case_id", "repeat", "arm", "order", "controller", "state", "measured", "accounting_complete", "accounting_unknown_reasons", "functional_status", "protocol_status", "verified_success", "elapsed_ns_exploratory"}, tokenHeader...), runs},
		{"threads.csv", append([]string{"run_id", "trace_sha256", "role", "stage_source", "known"}, tokenHeader...), threads},
		{"pairs.csv", []string{"case_id", "repeat", "normal_run", "compact_run", "normal_functional", "compact_functional", "normal_protocol", "compact_protocol", "accounting_comparable", "both_functional_success", "both_verified_success", "functional_loss", "normal_tokens", "compact_tokens", "compact_minus_normal", "compact_over_normal"}, pairs},
		{"arms.csv", armHeader, arms},
		{"cases.csv", append([]string{"case_id"}, armHeader...), cases},
		{"roles.csv", append([]string{"arm", "role", "threads", "known_threads"}, tokenHeader...), roles},
	} {
		if err := analysisWriteCSV(filepath.Join(output, file.name), file.header, file.rows); err != nil {
			return err
		}
	}
	return nil
}

// Run only the separately built analysis executable. Its output is posthoc and
// must never replace the frozen pilot.test used by the measured controllers.
func TestPilotAnalysis(t *testing.T) {
	if *pilotAnalysisOutput == "" {
		t.Skip("explicit analysis destination required; no model is invoked")
	}
	if *pilotOutput == "" {
		t.Fatal("analysis requires -pilot-output")
	}
	output, err := filepath.Abs(*pilotOutput)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := filepath.Abs(*pilotAnalysisOutput)
	if err != nil || destination == output || inside(filepath.Join(output, "runs"), destination) || inside(filepath.Join(output, "trusted-candidates"), destination) {
		t.Fatal("analysis destination must be separate from solver and candidate artifacts")
	}
	m, p, rows, err := analysisLoad(output)
	if err != nil {
		t.Fatal(err)
	}
	report := analysisBuild(m, p, rows)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	report.ExecutableSHA256, err = hashFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := analysisExport(destination, report); err != nil {
		t.Fatal(err)
	}
	t.Logf("sanitized %d assigned rows; all_results=%v; export=%s", len(rows), report.AllResults, destination)
}

func TestPilotAnalysisActualTokensAndUnknownOutcomes(t *testing.T) {
	parentReasoning, childReasoning := int64(5), int64(8)
	selected := cell{ID: "case-r1-normal", CaseID: "case", Repeat: 1, Arm: "normal"}
	result := runResult{Run: selected, Measured: true, Status: "timed_out", Method: methodReport{Status: "unknown"},
		Grade: json.RawMessage(`{"infrastructure_status":"ok","eligibility":{"passed":true},"acceptance":{"passed":false},"regressions":{"passed":true},"patch_integrity":{"passed":true}}`),
		Usage: usageReport{Complete: true, Total: 190, ReasoningOutput: func() *int64 { value := int64(13); return &value }(), Counts: tokens.Counts{Input: 140, CacheRead: 20, Output: 30},
			Threads: []traceInfo{{ID: "parent", Known: true, Counts: tokens.Counts{Input: 100, CacheRead: 20, Output: 10}, ReasoningOutput: &parentReasoning},
				{ID: "child", ParentID: "parent", Known: true, Stage: "code", Counts: tokens.Counts{Input: 40, Output: 20}, ReasoningOutput: &childReasoning}}}}
	row, err := analysisObserved(selected, &result)
	if err != nil {
		t.Fatal(err)
	}
	if row.Tokens.Total != 190 || *row.Tokens.ReasoningOutput != 13 || row.Threads[0].Role != "coordinator" || row.Threads[1].Role != "code" || row.FunctionalStatus != "failed" || row.VerifiedSuccess {
		t.Fatal("lost failed costs, double-counted reasoning, or blessed an unknown audit", row)
	}
	missing, err := analysisObserved(cell{ID: "case-r1-compact", CaseID: "case", Repeat: 1, Arm: "compact"}, nil)
	if err != nil || missing.Tokens != nil || missing.AccountingComplete || missing.FunctionalStatus != "unknown" {
		t.Fatal("missing usage became zero or success", missing, err)
	}
	m := manifest{Cases: []pilotCase{{ID: "case"}}, Repeats: 1}
	report := analysisBuild(m, plan{Runs: []cell{selected, {ID: missing.RunID}}}, []analysisRun{row, missing})
	if report.Arms[0].Tokens.Total != 190 || report.Subsets[0].InferenceReady || report.Subsets[0].TokenRatio != nil || report.TaskLoss.UnassessedTasks != 1 {
		t.Fatal("partial evidence produced complete inference", report)
	}
	result.Usage.Complete, result.Usage.Threads[1].ReasoningOutput = false, nil
	result.Usage.ReasoningOutput = nil
	partial, err := analysisObserved(selected, &result)
	if err != nil || partial.AccountingComplete || partial.Tokens.ReasoningOutput != nil {
		t.Fatal("unknown accounting was promoted", partial, err)
	}
}

func TestPilotAnalysisClusterIntervalsAndLossBound(t *testing.T) {
	m := manifest{Seed: 20261116, Repeats: 3}
	pairs := []analysisPair{}
	for task := 0; task < 12; task++ {
		id := fmt.Sprintf("case-%d", task)
		m.Cases = append(m.Cases, pilotCase{ID: id, Provenance: json.RawMessage(fmt.Sprintf(`{"repo":"repo-%d"}`, task%7))})
		for repeat := 1; repeat <= 3; repeat++ {
			normal, compact, loss := int64(200+task*20), int64(100+task*10), false
			pairs = append(pairs, analysisPair{CaseID: id, Repeat: repeat, AccountingComparable: true, NormalTokens: &normal, CompactTokens: &compact,
				NormalFunctional: "passed", CompactFunctional: "passed", NormalProtocol: "verified", CompactProtocol: "verified", BothFunctionalSuccess: true, BothVerifiedSuccess: true, FunctionalLoss: &loss})
		}
	}
	groups := analysisSubsets(m, pairs)
	for _, group := range groups {
		if !group.InferenceReady || group.SelectedPairs != 36 || group.TokenRatio == nil || group.TokenRatio.Clusters != 12 || group.TokenRatio.Estimate != .5 || group.TokenRatio.Lower != .5 || group.TokenRatio.Upper != .5 {
			t.Fatal("lost task clustering or deterministic ratio", group)
		}
		if group.RepoTokenRatio == nil || group.RepoTokenRatio.Clusters != 7 || group.RepoTokenRatio.Unit != "repository" || group.RepoTokenRatio.Estimate != .5 {
			t.Fatal("lost related-repository sensitivity", group)
		}
	}
	quality := analysisQualityCI(m, pairs, false, "task")
	if quality == nil || quality.Clusters != 12 || quality.Estimate != 0 || quality.Lower != 0 || quality.Upper != 0 {
		t.Fatal("paired functional outcome interval is wrong", quality)
	}
	first, _ := json.Marshal(groups)
	second, _ := json.Marshal(analysisSubsets(m, pairs))
	if string(first) != string(second) {
		t.Fatal("same input and seed produced different intervals")
	}
	bound := analysisTaskLoss(m, pairs)
	if bound.KnownLossTasks != 0 || bound.FullyAssessedTasks != 12 || bound.AssessedLossUpper95 == nil || math.Abs(*bound.AssessedLossUpper95-.22092219194555585) > 1e-12 {
		t.Fatal("zero/12 task bound is wrong", bound)
	}
	loss := true
	pairs[0].FunctionalLoss, pairs[1].FunctionalLoss = &loss, nil
	bound = analysisTaskLoss(m, pairs)
	if bound.KnownLossTasks != 1 || bound.UnassessedTasks != 0 || bound.FullyAssessedTasks != 11 || bound.ConservativeLossUpper95 <= .22 {
		t.Fatal("missing repeat erased an established task loss", bound)
	}
	if analysisBinomialUpper(12, 12, .05) != 1 || analysisBinomialUpper(0, 0, .05) != 1 {
		t.Fatal("boundary loss bounds are wrong")
	}
	if math.Abs(analysisBinomialUpper(1, 2, .05)-math.Sqrt(.95)) > 1e-12 {
		t.Fatal("positive-loss exact bound is wrong")
	}
}

func TestPilotAnalysisUsesFrozenStudySizeAndEffort(t *testing.T) {
	for _, test := range []struct {
		name    string
		effort  string
		repeats int
	}{
		{name: "high-24", effort: "high", repeats: 1},
		{name: "max-72", effort: "max", repeats: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := manifest{Version: 1, ID: test.name, Model: "gpt-6.1-sol", Effort: test.effort, Repeats: test.repeats, Controllers: 6, Seed: 20261116}
			for task := 0; task < 12; task++ {
				m.Cases = append(m.Cases, pilotCase{ID: fmt.Sprintf("case-%d", task)})
			}
			output := t.TempDir()
			manifestPath := filepath.Join(output, "frozen-manifest.json")
			if err := writeJSON(manifestPath, m, true); err != nil {
				t.Fatal(err)
			}
			digest, err := hashFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeJSON(filepath.Join(output, "plan.json"), schedule(m, digest), true); err != nil {
				t.Fatal(err)
			}
			frozen, planned, rows, err := analysisLoad(output)
			if err != nil {
				t.Fatal(err)
			}
			report := analysisBuild(frozen, planned, rows)
			if len(report.Runs) != 24*test.repeats || len(report.Pairs) != 12*test.repeats || report.Model != m.Model || report.Effort != test.effort || report.AllResults {
				t.Fatal("analysis did not preserve frozen assignments and model settings", report)
			}
			for _, arm := range report.Arms {
				if arm.Assigned != 12*test.repeats || arm.FunctionalUnknown != arm.Assigned || arm.AllAccountingComplete {
					t.Fatal("pending assignments lost or promoted to observed outcomes", arm)
				}
			}
		})
	}
}

func TestPilotAnalysisInterruptedStreamHasUnknownTailUsage(t *testing.T) {
	for _, test := range []struct {
		name   string
		call   invocation
		status string
		reason string
	}{
		{name: "normal-exit-blocked-task", call: invocation{ExitCode: 0}, status: "completed"},
		{name: "forced-stop", call: invocation{ExitCode: 0, Stopped: true}, status: "completed", reason: "tail_usage_unknown_after_forced_stop"},
		{name: "cancelled", call: invocation{ExitCode: 0, Cancelled: true}, status: "completed", reason: "tail_usage_unknown_after_cancellation"},
		{name: "timeout-status", call: invocation{ExitCode: 0}, status: "timed_out", reason: "tail_usage_unknown_after_forced_stop"},
		{name: "cleanup-failed", call: invocation{ExitCode: 0, Cleanup: "process-tree cleanup failed"}, status: "completed", reason: "tail_usage_unknown_after_cleanup_failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			counts, reasoning := tokens.Counts{Input: 20, CacheRead: 3, Output: 5}, int64(2)
			selected := cell{ID: "case-r1-compact", CaseID: "case", Repeat: 1, Arm: "compact"}
			result := runResult{Run: selected, Measured: true, Status: test.status, Execution: &test.call, Usage: usageReport{Complete: true, Counts: counts, Total: counts.Sum(), ReasoningOutput: &reasoning,
				Threads: []traceInfo{{ID: "root", Known: true, Counts: counts, ReasoningOutput: &reasoning}}}}
			row, err := analysisObserved(selected, &result)
			if err != nil || row.Tokens == nil || row.Tokens.Total != 28 || *row.Tokens.ReasoningOutput != 2 {
				t.Fatal("interruption lost recorded lower-bound usage", row, err)
			}
			if test.reason == "" {
				if !row.AccountingComplete {
					t.Fatal("normal exit lost known accounting", row)
				}
			} else if row.AccountingComplete || !slices.Contains(row.AccountingUnknownReasons, test.reason) {
				t.Fatal("captured counters incorrectly certified interrupted actual total", row)
			}
			normal := row
			normal.RunID, normal.Arm, normal.AccountingComplete = "case-r1-normal", "normal", true
			pairs := analysisPairs(manifest{Cases: []pilotCase{{ID: "case"}}, Repeats: 1}, []analysisRun{normal, row})
			if test.reason != "" && (pairs[0].AccountingComparable || pairs[0].TokenRatio != nil) {
				t.Fatal("unknown stream tail entered complete paired token comparison", pairs)
			}
		})
	}
}

func TestPilotAnalysisSanitizedDeterministicExports(t *testing.T) {
	selected := cell{ID: "case-r1-normal", CaseID: "case", Repeat: 1, Arm: "normal"}
	result := runResult{Run: selected, Status: "preparation_failed", Error: "/private/home/auth.json secret-value", Prompt: asset{Path: "/private/prompt"}, Method: methodReport{Status: "unknown"}}
	row, err := analysisObserved(selected, &result)
	if err != nil {
		t.Fatal(err)
	}
	row.ResultSHA256 = "result-hash"
	m := manifest{Cases: []pilotCase{{ID: "case", Snapshot: "/private/source", Provenance: json.RawMessage(`{"private":"secret-value"}`)}}, Repeats: 1}
	report := analysisBuild(m, plan{Runs: []cell{selected}}, []analysisRun{row})
	root := t.TempDir()
	if err := analysisExport(root, report); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"analysis.json", "runs.csv", "threads.csv", "pairs.csv", "arms.csv", "cases.csv", "roles.csv"} {
		before, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc([]string{"/private/", "secret-value", "auth.json"}, func(secret string) bool { return bytes.Contains(before, []byte(secret)) }) {
			t.Fatalf("private source data leaked into %s", name)
		}
		if err := analysisExport(root, report); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(before) != string(after) {
			t.Fatalf("non-deterministic export %s: %v", name, err)
		}
		if filepath.Ext(name) == ".csv" {
			f, err := os.Open(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			reader := csv.NewReader(f)
			for {
				_, readErr := reader.Read()
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					t.Fatal(readErr)
				}
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}
