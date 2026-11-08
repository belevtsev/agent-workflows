// Package evals holds the offline checks for the `claude plugin eval` suites in
// this directory. Its tests validate every case's files against the runner's
// schema, check a finished run's aggregate-result.json against a pass-rate
// floor, and run the mutant gate that executes a test-engineer agent's authored
// tests against mutated copies of the case's implementation fixture. See
// README.md for how to run them.
package evals
