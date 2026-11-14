package batch

const DefaultLimit = 50
const HardLimit = 500

func ResolveLimit(configured int) int {
	if configured == 0 {
		return DefaultLimit
	}
	return configured
}
