package pilot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
)

const (
	verifierCorpusHash   = "1263da9e024c8c2e6002a9ee9f7356ae0acbcb043c1ec2a5db03e516aa69382a"
	verifierContractHash = "2b147315848aec204bfc21771bd63dadf28e57af153c3d2a392965fbd4fb2b08"
)

type verifierCorpus struct {
	Version    int              `json:"version"`
	Provenance string           `json:"provenance"`
	Partition  string           `json:"partition"`
	Sources    []verifierSource `json:"sources"`
}

type verifierSource struct {
	ID     string            `json:"id"`
	Split  string            `json:"split"`
	File   string            `json:"file"`
	SHA256 string            `json:"sha256"`
	Labels map[string]string `json:"labels"`
}

func verifierDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// Labels and the public contract are frozen independently of the verdict below.
// The source reader accepts only repository-relative, fixed corpus filenames.
func loadVerifierCorpus(read func(string) ([]byte, error)) (verifierCorpus, error) {
	var corpus verifierCorpus
	data, err := read("testdata/verifier-calibration-v1.json")
	if err != nil || verifierDigest(data) != verifierCorpusHash {
		return corpus, errors.New("calibration label corpus hash mismatch or missing")
	}
	if err := verifierJSON(data, &corpus); err != nil {
		return corpus, err
	}
	if corpus.Version != 1 || len(corpus.Sources) != 4 || corpus.Provenance == "" || corpus.Partition == "" {
		return corpus, errors.New("calibration label corpus version or provenance incomplete")
	}
	seen := map[string]bool{}
	for _, source := range corpus.Sources {
		if seen[source.ID] || len(source.Labels) != len(charsetV2Cases()) ||
			(source.Split != "development" && source.Split != "validation") {
			return corpus, errors.New("calibration labels incomplete, duplicated or unpartitioned")
		}
		seen[source.ID] = true
		for _, c := range charsetV2Cases() {
			if source.Labels[c.ID] != "correct" && source.Labels[c.ID] != "defect" {
				return corpus, errors.New("calibration behavior lacks explicit label")
			}
		}
	}
	return corpus, nil
}

func verifierContract(read func(string) ([]byte, error)) error {
	data, err := read("charset-v2-contract.json")
	if err != nil || verifierDigest(data) != verifierContractHash {
		return errors.New("public contract hash mismatch or missing")
	}
	var contract []charsetV2Case
	if err := verifierJSON(data, &contract); err != nil {
		return err
	}
	if !reflect.DeepEqual(contract, charsetV2Cases()) {
		return errors.New("revised verifier differs from pinned public contract")
	}
	return nil
}

// Unlike ordinary json.Unmarshal, this rejects repeated members, trailing JSON,
// absent zero-valued fields and unknown fields. Historical stdout uses uppercase
// row keys, so duplicate detection folds case just as struct decoding does.
func verifierJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := verifierJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing calibration JSON")
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid calibration JSON fields")
	}
	return nil
}

func verifierJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("calibration JSON nesting exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return errors.New("malformed calibration JSON")
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	keys := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			token, err := decoder.Token()
			if err != nil {
				return errors.New("malformed calibration JSON key")
			}
			key, ok := token.(string)
			if !ok || keys[strings.ToLower(key)] {
				return errors.New("duplicate calibration JSON member")
			}
			keys[strings.ToLower(key)] = true
		}
		if err := verifierJSONValue(decoder, depth+1); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return errors.New("unclosed calibration JSON")
	}
	return nil
}

func verifierObserved(data []byte) (charsetV2Observed, error) {
	var wire struct {
		Version *int    `json:"version"`
		Go      *string `json:"go_version"`
		Rows    *[]struct {
			ID     *string `json:"id"`
			Status *int    `json:"status"`
			Calls  *int    `json:"calls"`
			Body   *string `json:"body"`
		} `json:"rows"`
	}
	var observed charsetV2Observed
	if err := verifierJSON(data, &wire); err != nil {
		return observed, err
	}
	if wire.Version == nil || wire.Go == nil || wire.Rows == nil {
		return observed, errors.New("executed evidence metadata missing")
	}
	observed.Version, observed.Go = *wire.Version, *wire.Go
	for _, row := range *wire.Rows {
		if row.ID == nil || row.Status == nil || row.Calls == nil || row.Body == nil {
			return observed, errors.New("executed behavior field missing")
		}
		observed.Rows = append(observed.Rows, charsetV2Row{ID: *row.ID, Status: *row.Status, Calls: *row.Calls, Body: *row.Body})
	}
	return observed, nil
}

type verifierCounts struct {
	Defects      int `json:"labeled_defects"`
	Controls     int `json:"labeled_correct_behaviors"`
	Detected     int `json:"detected_defects"`
	Correct      int `json:"accepted_correct_behaviors"`
	Misses       int `json:"misses"`
	FalseAlarms  int `json:"false_alarms"`
	Unknown      int `json:"unknown"`
	CoverageGaps int `json:"original_coverage_defects_outside_checks"`
}

type verifierResult struct {
	Source   string `json:"source"`
	Split    string `json:"split"`
	Behavior string `json:"behavior"`
	Label    string `json:"label"`
	Verdict  string `json:"revised_verdict"`
}

type verifierReceipt struct {
	Version    int                       `json:"version"`
	CorpusHash string                    `json:"corpus_sha256"`
	Evidence   map[string]string         `json:"evidence_sha256"`
	Verifier   string                    `json:"verifier"`
	Complete   bool                      `json:"complete"`
	Provenance string                    `json:"provenance"`
	Partition  string                    `json:"partition"`
	Scope      string                    `json:"scope"`
	Splits     map[string]verifierCounts `json:"splits"`
	Results    []verifierResult          `json:"results"`
	Problems   []string                  `json:"problems"`
}

func calibrateVerifier(read func(string) ([]byte, error)) verifierReceipt {
	receipt := verifierReceipt{Version: 1, CorpusHash: verifierCorpusHash, Verifier: "charset-v2-complete-evidence-v1",
		Scope:    "Offline retrospective behavior calibration only; no blind heldout, independent repository, fully correct candidate, model quality or compact equivalence claim. Original coverage replay is not execution of the frozen old grader; prior grades unchanged.",
		Evidence: map[string]string{"charset-v2-contract.json": verifierContractHash},
		Splits:   map[string]verifierCounts{}, Results: []verifierResult{}, Problems: []string{}}
	corpus, err := loadVerifierCorpus(read)
	if err != nil {
		receipt.Problems = append(receipt.Problems, err.Error())
		receipt.Splits["unassigned"] = verifierCounts{Unknown: 44}
		return receipt
	}
	receipt.Provenance, receipt.Partition = corpus.Provenance, corpus.Partition
	contractErr := verifierContract(read)
	if contractErr != nil {
		receipt.Problems = append(receipt.Problems, contractErr.Error())
	}
	for _, source := range corpus.Sources {
		receipt.Evidence[source.File] = source.SHA256
		failures, err := verifierSourceFailures(read, source)
		if contractErr != nil {
			err = contractErr
		}
		if err != nil {
			receipt.Problems = append(receipt.Problems, fmt.Sprintf("%s: evidence unknown", source.ID))
		}
		for _, c := range charsetV2Cases() {
			verdict := "accept"
			if err != nil {
				verdict = "unknown"
			} else if slices.Contains(failures, c.ID) {
				verdict = "reject"
			}
			result := verifierResult{source.ID, source.Split, c.ID, source.Labels[c.ID], verdict}
			receipt.Results = append(receipt.Results, result)
			counts := receipt.Splits[source.Split]
			counts.record(result, c.Family)
			receipt.Splits[source.Split] = counts
		}
	}
	receipt.Complete = len(receipt.Problems) == 0
	return receipt
}

func verifierSourceFailures(read func(string) ([]byte, error), source verifierSource) ([]string, error) {
	data, err := read(source.File)
	if err != nil || verifierDigest(data) != source.SHA256 {
		return nil, errors.New("executed evidence hash mismatch or missing")
	}
	observed, err := verifierObserved(data)
	if err != nil {
		return nil, err
	}
	return charsetV2Failures(observed)
}

func (counts *verifierCounts) record(result verifierResult, family string) {
	defect := result.Label == "defect"
	if defect {
		counts.Defects++
		if family == "extended_override" || family == "unquoted_stray_quote" {
			counts.CoverageGaps++
		}
	} else {
		counts.Controls++
	}
	switch {
	case result.Verdict == "unknown":
		counts.Unknown++
	case defect && result.Verdict == "reject":
		counts.Detected++
	case defect:
		counts.Misses++
	case result.Verdict == "reject":
		counts.FalseAlarms++
	default:
		counts.Correct++
	}
}
