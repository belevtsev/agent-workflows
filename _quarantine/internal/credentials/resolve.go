package credentials

import "os"

func (resolver Resolver) getenv() func(string) string {
	if resolver.Getenv != nil {
		return resolver.Getenv
	}
	return os.Getenv
}

// Environment resolves a consistent set of adviser keys for one consumer. All
// other names pass through to the supplied environment reader. Nothing mutates
// the global environment, and fully supplied environment keys avoid disk reads.
func (resolver Resolver) Environment() (func(string) string, error) {
	getenv := resolver.getenv()
	effective := make(map[string]string, len(credentialNames))
	complete := true
	for _, name := range credentialNames {
		effective[name] = getenv(name)
		complete = complete && effective[name] != ""
	}
	if !complete {
		snapshot, err := (Store{Directory: resolver.Directory}).Read()
		if err != nil {
			return nil, err
		}
		for _, name := range credentialNames {
			if effective[name] == "" {
				effective[name] = snapshot.Keys[name]
			}
		}
	}
	return func(name string) string {
		if knownName(name) {
			return effective[name]
		}
		return getenv(name)
	}, nil
}

// Presence reports safe metadata only. It inspects saved storage even when all
// environment keys are present so doctor/status can surface unsafe saved state.
func (resolver Resolver) Presence() (map[string]Presence, error) {
	getenv := resolver.getenv()
	snapshot, err := (Store{Directory: resolver.Directory}).Read()
	if err != nil {
		return nil, err
	}
	result := make(map[string]Presence, len(credentialNames))
	for _, name := range credentialNames {
		environment, stored := getenv(name) != "", snapshot.Keys[name] != ""
		source := "none"
		if environment {
			source = "environment"
		} else if stored {
			source = "stored"
		}
		result[name] = Presence{Environment: environment, Stored: stored, Effective: environment || stored, Source: source}
	}
	return result, nil
}
