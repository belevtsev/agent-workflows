package pilot

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var verifierCalibrationOutput = flag.String("pilot-verifier-calibration-output", "", "new absolute sanitized offline verifier calibration receipt file; no models invoked")

func verifierPublicRead(name string) ([]byte, error) {
	if name == "testdata/verifier-calibration-v1.json" {
		return os.ReadFile(filepath.FromSlash(name))
	}
	return os.ReadFile(filepath.Join("..", "..", "docs", "benchmarks", "selective-compact-data", name))
}

func TestVerifierCalibration(t *testing.T) {
	t.Parallel()
	receipt := calibrateVerifier(verifierPublicRead)
	want := map[string]verifierCounts{
		"development": {Defects: 4, Controls: 18, Detected: 4, Correct: 18, CoverageGaps: 3},
		"validation":  {Defects: 8, Controls: 14, Detected: 8, Correct: 14, CoverageGaps: 8},
	}
	if !receipt.Complete || len(receipt.Results) != 44 || !reflect.DeepEqual(receipt.Splits, want) {
		t.Fatalf("historical labels disagree with revised verifier: %+v; %v", receipt.Splits, receipt.Problems)
	}
	if again := calibrateVerifier(verifierPublicRead); !reflect.DeepEqual(receipt, again) {
		t.Fatal("offline calibration is not reproducible")
	}
}

// This entry only replays checked-in public evidence. It never invokes models,
// builds source repositories, changes frozen inputs, or reads private paths.
func TestPilotVerifierCalibration(t *testing.T) {
	if *verifierCalibrationOutput == "" {
		t.Skip("explicit new output file required; ordinary calibration is TestVerifierCalibration")
	}
	receipt := calibrateVerifier(verifierPublicRead)
	if err := writeVerifierReceipt(*verifierCalibrationOutput, receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Complete {
		t.Fatalf("calibration contains unknown evidence: %v", receipt.Problems)
	}
	for split, counts := range receipt.Splits {
		if counts.Unknown != 0 || counts.Misses != 0 || counts.FalseAlarms != 0 {
			t.Fatalf("calibration disagreement in %s: %+v", split, counts)
		}
	}
	t.Logf("offline receipt: %s", *verifierCalibrationOutput)
}

func writeVerifierReceipt(path string, receipt verifierReceipt) error {
	if !filepath.IsAbs(path) || filepath.Ext(path) != ".json" {
		return errors.New("calibration output must be a new absolute .json file")
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	if err != nil {
		return err
	}
	return file.Close()
}

func TestVerifierCalibrationFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file string
		data []byte
	}{
		{name: "missing labels", file: "testdata/verifier-calibration-v1.json"},
		{name: "changed label version", file: "testdata/verifier-calibration-v1.json", data: []byte(`{"version":2}`)},
		{name: "missing contract", file: "charset-v2-contract.json"},
		{name: "changed contract", file: "charset-v2-contract.json", data: []byte(`[]`)},
		{name: "missing executed evidence", file: "charset-v2-candidate-2-stdout.json"},
		{name: "changed executed evidence", file: "charset-v2-candidate-2-stdout.json", data: []byte(`{"version":2}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			receipt := calibrateVerifier(func(name string) ([]byte, error) {
				if name == tt.file {
					if tt.data == nil {
						return nil, os.ErrNotExist
					}
					return tt.data, nil
				}
				return verifierPublicRead(name)
			})
			unknown := 0
			for _, counts := range receipt.Splits {
				unknown += counts.Unknown
			}
			if receipt.Complete || len(receipt.Problems) == 0 || unknown == 0 {
				t.Fatal("incomplete evidence became a passing calibration", receipt)
			}
			for _, result := range receipt.Results {
				if strings.Contains(tt.file, result.Source+"-stdout") && result.Verdict != "unknown" {
					t.Fatal("corrupt source received a verdict", result)
				}
			}
		})
	}
}

func TestVerifierEvidenceRejectsIncompleteJSON(t *testing.T) {
	t.Parallel()
	data, err := verifierPublicRead("charset-v2-baseline-stdout.json")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		data string
	}{
		{name: "malformed", data: "{"},
		{name: "null", data: "null"},
		{name: "trailing object", data: string(data) + "{}"},
		{name: "duplicate metadata", data: strings.Replace(string(data), `"version":2`, `"version":2,"Version":2`, 1)},
		{name: "duplicate row field", data: strings.Replace(string(data), `"Status":204`, `"Status":204,"status":204`, 1)},
		{name: "absent zero calls", data: strings.Replace(string(data), `"Calls":0,`, "", 1)},
		{name: "absent empty body", data: strings.Replace(string(data), `,"Body":""`, "", 1)},
		{name: "unknown field", data: strings.Replace(string(data), `"version":2`, `"version":2,"unexecuted":true`, 1)},
		{name: "wrong version", data: strings.Replace(string(data), `"version":2`, `"version":1`, 1)},
		{name: "absent runtime", data: strings.Replace(string(data), `"go_version":"go1.27.4",`, "", 1)},
		{name: "missing behavior", data: strings.Replace(string(data), `{"ID":"ordinary_allowed","Status":204,"Calls":1,"Body":"body"},`, "", 1)},
		{name: "duplicate behavior", data: strings.Replace(string(data), `"ID":"ordinary_allowed"`, `"ID":"ordinary_disallowed"`, 1)},
		{name: "unknown behavior", data: strings.Replace(string(data), `"ID":"ordinary_allowed"`, `"ID":"unexecuted"`, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			observed, err := verifierObserved([]byte(tt.data))
			if err == nil {
				_, err = charsetV2Failures(observed)
			}
			if err == nil {
				t.Fatal("incomplete or ambiguous execution evidence accepted")
			}
		})
	}
}

func TestVerifierCalibrationReceiptRefusesOverwrite(t *testing.T) {
	t.Parallel()
	receipt := calibrateVerifier(verifierPublicRead)
	path := filepath.Join(t.TempDir(), "calibration.json")
	if err := writeVerifierReceipt(path, receipt); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeVerifierReceipt(path, verifierReceipt{}); err == nil {
		t.Fatal("existing calibration receipt overwritten")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("existing calibration receipt changed", err)
	}
	if err := writeVerifierReceipt("relative.json", receipt); err == nil {
		t.Fatal("implicit relative output accepted")
	}
	for _, private := range []string{"/home/", "/workspace/", "/toolchain/", "started_at", "argv"} {
		if strings.Contains(string(before), private) {
			t.Fatal("receipt exposed execution paths or incidental private provenance", private)
		}
	}
}
