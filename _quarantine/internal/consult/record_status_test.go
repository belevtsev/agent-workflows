package consult

import "testing"

// Oracle: Run reads the status it prints through the typed accessors, so a
// record of another shape is an error before anything is written, not a
// panic after it.
func TestRecordStatusRefusesAnotherShapeWithoutPanicking(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		record       *object
		adjudication bool
		want         string
	}{
		{"consultation", newObject("status", "success"), false, "success"},
		{"adjudication", newObject("final_decision", newObject("status", "resolved")), true, "resolved"},
		{"status not text", newObject("status", number(1)), false, ""},
		{"no status", newObject(), false, ""},
		{"final decision not an object", newObject("final_decision", "resolved"), true, ""},
		{"final decision without status", newObject("final_decision", newObject()), true, ""},
	} {
		got, err := recordStatus(tc.record, tc.adjudication)
		if got != tc.want || (err == nil) != (tc.want != "") {
			t.Errorf("%s: recordStatus = %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}
