package greet

import "fmt"

// Greet returns the greeting for name.
func Greet(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("Helo: empty name")
	}
	return "Hello, " + name, nil
}
