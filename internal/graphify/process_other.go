//go:build !linux && !darwin

package graphify

func supported() error {
	return refuse("platform", "The guarded Graphify runner currently supports macOS and Linux")
}
