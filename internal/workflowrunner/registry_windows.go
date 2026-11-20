package workflowrunner

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

func readRegistration() ([]byte, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Kitsu`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return nil, errors.New("configure a trusted workflow runner in HKLM\\SOFTWARE\\Kitsu\\WorkflowRunners")
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = key.Close() }()
	value, _, err := key.GetStringValue("WorkflowRunners")
	if len(value) > 1<<20 {
		return nil, errors.New("workflow runner registration is too large")
	}
	return []byte(value), err
}
