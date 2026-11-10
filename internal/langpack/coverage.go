package langpack

import "slices"

// CreditsGoTest reports whether go.package-test/v1 credits a passed run of
// argv with empty GOFLAGS.
func CreditsGoTest(argv []string) bool { return credited(Run{Argv: argv}) != nil }

// GoWork is Go work the Go rule tracks: package directories and module files,
// each relative to the selected path, sorted, never nil.
type GoWork struct {
	Packages    []string
	ModuleFiles []string
}

// AddedGoCoverage returns, for each argv in added in order, the qualifying
// items it covers that no argv in prior and no earlier argv in added covers.
// merged, like source.Current and source.Changed, is slash-separated and
// relative to the selected path.
func AddedGoCoverage(source Source, merged []string, prior, added [][]string) []GoWork {
	// A qualifying item is changed since the baseline and holds a merged
	// file; both sides are mapped with the current files, as GoTests maps them.
	changedPackages, changedModuleFiles := changedWork(source)
	mergedPackages, mergedModuleFiles := changedWork(Source{Current: source.Current, Changed: merged})
	packages := intersect(changedPackages, mergedPackages)
	moduleFiles := intersect(changedModuleFiles, mergedModuleFiles)
	coveredPackages, coveredModuleFiles := map[string]bool{}, map[string]bool{}
	cover := func(argv []string) GoWork {
		work := GoWork{Packages: []string{}, ModuleFiles: []string{}}
		patterns := credited(Run{Argv: argv})
		for _, name := range moduleFiles {
			if !coveredModuleFiles[name] && slices.Contains(patterns, "...") {
				coveredModuleFiles[name] = true
				work.ModuleFiles = append(work.ModuleFiles, name)
			}
		}
		for _, pkg := range packages {
			if !coveredPackages[pkg] && slices.ContainsFunc(patterns, func(pattern string) bool { return covers(pattern, pkg) }) {
				coveredPackages[pkg] = true
				work.Packages = append(work.Packages, pkg)
			}
		}
		return work
	}
	for _, argv := range prior {
		cover(argv)
	}
	result := make([]GoWork, 0, len(added))
	for _, argv := range added {
		result = append(result, cover(argv))
	}
	return result
}

// intersect returns the elements of the sorted slice a that b also holds, in
// a's order.
func intersect(a, b []string) []string {
	return slices.DeleteFunc(slices.Clone(a), func(item string) bool { return !slices.Contains(b, item) })
}
