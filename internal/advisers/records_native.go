package advisers

import (
	"errors"
	"reflect"
)

// Re-derive structured results offline so altered selections or token counts
// cannot become valid receipts independently of the immutable provider answer.
func validateTypedExecution(c Consultation) error {
	e := c.Execution
	if c.Version == 1 && (c.Identity.Provider != "" || c.Request.Version != 1 || e.Version != 0) {
		return errors.New("native and typed consultations require version 2")
	}
	if c.Version == 2 && e.Version != 2 {
		return errors.New("version 2 consultation requires version 2 execution")
	}
	if c.Identity.Provider == "" {
		if e.Result != nil || e.Usage != nil || e.UsageKnown || e.ActualModel != "unknown" {
			return errors.New("CLI consultations cannot claim structured results, model identity or usage")
		}
		return nil
	}
	return validateNativeExecution(c)
}

func validateNativeExecution(c Consultation) error {
	e := c.Execution
	if e.ExitStatus != nil {
		return errors.New("native execution cannot have a process exit status")
	}
	if e.Answer == "" {
		if e.Result != nil || e.Usage != nil || e.UsageKnown || e.ActualModel != "unknown" {
			return errors.New("native execution without an answer cannot claim results or usage")
		}
		return nil
	}
	result, model, usage, err := decodeNativeResponse([]byte(e.Answer), c.Request)
	if err != nil || !reflect.DeepEqual(result, e.Result) || !reflect.DeepEqual(usage, e.Usage) ||
		e.ActualModel != model || e.UsageKnown != (usage != nil) {
		return errors.New("native execution does not match its provider answer and request")
	}
	return nil
}
