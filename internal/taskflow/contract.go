package taskflow

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A test-engineer contract for Go is built from declarations extracted from the
// source rather than written from memory. ExtractContract prints only the
// requested symbols, always without function bodies (including function
// literals in var values) and without comments inside those bodies, and records
// the SHA-256 of every file a printed declaration comes from. A test-engineer
// brief carries the saved extract as its contract; assign refuses an extract
// that differs from what the current source yields, and verify reports a
// current contract whose declarations have since changed.

const maxContractSymbols = 256

var contractFileName = regexp.MustCompile(`^contract-[A-Za-z][A-Za-z0-9_-]{0,63}-[0-9a-f]{32}\.json$`)

// ContractRef names the saved contract extract a test-engineer brief carries.
type ContractRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Contract is the output of cw task contract-extract. Root is the absolute
// directory of the nearest go.mod at or above the package (the package
// directory itself when there is none); Dir and every Path are slash-separated
// and relative to Root.
type Contract struct {
	Language     string                `json:"language"`
	Root         string                `json:"root"`
	Dir          string                `json:"dir"`
	Symbols      []string              `json:"symbols"`
	Files        []ContractFile        `json:"files"`
	Declarations []ContractDeclaration `json:"declarations"`
}

type ContractFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// ContractDeclaration is one printed declaration. Kind is func, method, type,
// const or var. Text holds the doc comment and the declaration without a body.
type ContractDeclaration struct {
	Symbol string `json:"symbol"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Text   string `json:"text"`
}

// EncodeContract returns the exact bytes cw task contract-extract prints.
func EncodeContract(contract Contract) ([]byte, error) {
	data, err := json.Marshal(contract, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// ParseContractSymbols splits a comma-separated --symbols value. Each symbol is
// a Go identifier or Type.Method; blanks and duplicates are errors.
func ParseContractSymbols(value string) ([]string, error) {
	symbols := []string{}
	for raw := range strings.SplitSeq(value, ",") {
		symbol := strings.TrimSpace(raw)
		receiver, method, isMethod := strings.Cut(symbol, ".")
		if !token.IsIdentifier(receiver) || isMethod && !token.IsIdentifier(method) {
			return nil, fmt.Errorf("symbol %q is not a Go identifier or Type.Method", symbol)
		}
		if slices.Contains(symbols, symbol) {
			return nil, fmt.Errorf("symbol %s is repeated", symbol)
		}
		symbols = append(symbols, symbol)
	}
	if len(symbols) > maxContractSymbols {
		return nil, fmt.Errorf("at most %d symbols per extract", maxContractSymbols)
	}
	return symbols, nil
}

type contractSource struct {
	path string // relative to the contract root, slash-separated
	data []byte
	file *ast.File
	// stripped holds the original body ranges of the file's function
	// literals, emptied once after parsing; comments inside them are dropped.
	stripped [][2]token.Pos
}

// ExtractContract parses the non-test Go files directly in dir (every build
// constraint included, symlinked files followed, names starting with . or _
// ignored as go build does) and returns the requested declarations. Every
// symbol must be declared at least once; a symbol declared in several files (for
// several platforms) is printed once per file.
func ExtractContract(dir string, symbols []string) (Contract, error) {
	if len(symbols) == 0 {
		return Contract{}, errors.New("contract-extract needs at least one symbol")
	}
	if _, err := ParseContractSymbols(strings.Join(symbols, ",")); err != nil {
		return Contract{}, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Contract{}, err
	}
	if abs, err = filepath.EvalSymlinks(abs); err != nil {
		return Contract{}, err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return Contract{}, fmt.Errorf("contract-extract --path %s is not a directory", dir)
	}
	root := moduleRoot(abs)
	relativeDir, err := filepath.Rel(root, abs)
	if err != nil {
		return Contract{}, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return Contract{}, err
	}
	fset := token.NewFileSet()
	sources := []contractSource{}
	for _, entry := range entries {
		name := entry.Name()
		// go build ignores names starting with . or _ (editor lock files such
		// as a dangling .#a.go among them) and _test.go files.
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		// Follow a symlinked source file as go build does; skip directories
		// and other non-regular entries.
		if info, err := os.Stat(filepath.Join(abs, name)); err != nil {
			return Contract{}, err
		} else if !info.Mode().IsRegular() {
			continue
		}
		data, err := readContractSource(filepath.Join(abs, name))
		if err != nil {
			return Contract{}, err
		}
		relative := filepath.ToSlash(filepath.Join(relativeDir, name))
		file, err := parser.ParseFile(fset, relative, data, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return Contract{}, fmt.Errorf("parse %s: %w", relative, err)
		}
		sources = append(sources, contractSource{path: relative, data: data, file: file, stripped: stripFuncLits(file)})
	}
	if len(sources) == 0 {
		return Contract{}, fmt.Errorf("no non-test Go source in %s; contract-extract supports Go only", dir)
	}
	contract := Contract{Language: "go", Root: root, Dir: filepath.ToSlash(relativeDir), Symbols: slices.Clone(symbols), Files: []ContractFile{}, Declarations: []ContractDeclaration{}}
	used := map[string]bool{}
	missing := []string{}
	for _, symbol := range symbols {
		found := false
		for _, source := range sources {
			declarations, err := extractDeclarations(fset, source, symbol)
			if err != nil {
				return Contract{}, err
			}
			for _, declaration := range declarations {
				declaration.Path = source.path
				contract.Declarations = append(contract.Declarations, declaration)
				used[source.path] = true
				found = true
			}
		}
		if !found {
			missing = append(missing, symbol)
		}
	}
	if len(missing) > 0 {
		return Contract{}, fmt.Errorf("unknown symbol %s in %s", strings.Join(missing, ", "), dir)
	}
	for _, source := range sources {
		if used[source.path] {
			contract.Files = append(contract.Files, ContractFile{Path: source.path, SHA256: digest(source.data)})
		}
	}
	return contract, nil
}

func readContractSource(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxInputBytes {
		return nil, fmt.Errorf("%s exceeds the 8 MiB limit", path)
	}
	return data, nil
}

// moduleRoot returns the nearest directory at or above dir holding go.mod, or
// dir itself when none does.
func moduleRoot(dir string) string {
	for current := dir; ; {
		if info, err := os.Stat(filepath.Join(current, "go.mod")); err == nil && info.Mode().IsRegular() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return dir
		}
		current = parent
	}
}

func receiverName(expr ast.Expr) string {
	for {
		switch value := expr.(type) {
		case *ast.StarExpr:
			expr = value.X
		case *ast.ParenExpr:
			expr = value.X
		case *ast.IndexExpr:
			expr = value.X
		case *ast.IndexListExpr:
			expr = value.X
		case *ast.Ident:
			return value.Name
		default:
			return ""
		}
	}
}

func extractDeclarations(fset *token.FileSet, source contractSource, symbol string) ([]ContractDeclaration, error) {
	file := source.file
	receiver, method, isMethod := strings.Cut(symbol, ".")
	declarations := []ContractDeclaration{}
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if decl.Recv == nil && (isMethod || decl.Name.Name != symbol) {
				continue
			}
			if decl.Recv != nil && (!isMethod || decl.Name.Name != method || len(decl.Recv.List) != 1 || receiverName(decl.Recv.List[0].Type) != receiver) {
				continue
			}
			kind := "func"
			if decl.Recv != nil {
				kind = "method"
			}
			text, err := renderFunc(fset, source, decl)
			if err != nil {
				return nil, err
			}
			declarations = append(declarations, ContractDeclaration{Symbol: symbol, Kind: kind, Line: fset.Position(decl.Pos()).Line, Text: text})
		case *ast.GenDecl:
			if isMethod {
				continue
			}
			for index, spec := range decl.Specs {
				var declaration *ContractDeclaration
				var err error
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					if spec.Name.Name == symbol {
						declaration = &ContractDeclaration{Symbol: symbol, Kind: "type", Line: fset.Position(spec.Pos()).Line}
						declaration.Text, err = renderSpec(fset, source, decl, spec)
					}
				case *ast.ValueSpec:
					if slices.ContainsFunc(spec.Names, func(name *ast.Ident) bool { return name.Name == symbol }) {
						declaration = &ContractDeclaration{Symbol: symbol, Kind: decl.Tok.String(), Line: fset.Position(spec.Pos()).Line}
						declaration.Text, err = renderValueSpec(fset, source, decl, index, symbol)
					}
				}
				if err != nil {
					return nil, err
				}
				if declaration != nil {
					declarations = append(declarations, *declaration)
				}
			}
		}
	}
	return declarations, nil
}

// commentsBetween returns the file's comments wholly inside [start, end]
// that are not inside a stripped function literal body.
func commentsBetween(source contractSource, start, end token.Pos) []*ast.CommentGroup {
	return commentsWithin(source, [][2]token.Pos{{start, end}})
}

// commentsWithin returns the file's comments wholly inside one of the ranges
// that are not inside a stripped function literal body.
func commentsWithin(source contractSource, ranges [][2]token.Pos) []*ast.CommentGroup {
	groups := []*ast.CommentGroup{}
	covers := func(r [2]token.Pos, group *ast.CommentGroup) bool { return group.Pos() >= r[0] && group.End() <= r[1] }
	for _, group := range source.file.Comments {
		kept := slices.ContainsFunc(ranges, func(r [2]token.Pos) bool { return covers(r, group) })
		if kept && !slices.ContainsFunc(source.stripped, func(r [2]token.Pos) bool { return covers(r, group) }) {
			groups = append(groups, group)
		}
	}
	return groups
}

// stripFuncLits empties every function literal body under node and returns
// the original body ranges, so comments inside them can be dropped. It runs
// once per parsed file: a second pass would see only the emptied bodies.
func stripFuncLits(node ast.Node) [][2]token.Pos {
	ranges := [][2]token.Pos{}
	ast.Inspect(node, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit.Body != nil {
			ranges = append(ranges, [2]token.Pos{lit.Body.Lbrace, lit.Body.End()})
			lit.Body = &ast.BlockStmt{Lbrace: lit.Body.Lbrace, Rbrace: lit.Body.Lbrace + 1}
			return false
		}
		return true
	})
	return ranges
}

func printNode(fset *token.FileSet, node any) (string, error) {
	var buffer bytes.Buffer
	config := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := config.Fprint(&buffer, fset, node); err != nil {
		return "", err
	}
	return strings.TrimSpace(buffer.String()), nil
}

func renderFunc(fset *token.FileSet, source contractSource, decl *ast.FuncDecl) (string, error) {
	start := decl.Pos()
	if decl.Doc != nil {
		start = decl.Doc.Pos()
	}
	stripped := *decl
	stripped.Body = nil
	return printNode(fset, &printer.CommentedNode{Node: &stripped, Comments: commentsBetween(source, start, decl.Type.End())})
}

// specDoc returns a spec's own doc comment, or the declaration's when the
// declaration is not parenthesized.
func specDoc(decl *ast.GenDecl, doc *ast.CommentGroup) *ast.CommentGroup {
	if doc == nil && !decl.Lparen.IsValid() {
		return decl.Doc
	}
	return doc
}

func renderSpec(fset *token.FileSet, source contractSource, decl *ast.GenDecl, spec *ast.TypeSpec) (string, error) {
	doc := specDoc(decl, spec.Doc)
	start, end := spec.Pos(), spec.End()
	if doc != nil {
		start = doc.Pos()
	}
	if spec.Comment != nil {
		end = spec.Comment.End()
	}
	copied := *spec
	copied.Doc = nil
	single := &ast.GenDecl{Doc: doc, TokPos: spec.Pos(), Tok: decl.Tok, Specs: []ast.Spec{&copied}}
	return printNode(fset, &printer.CommentedNode{Node: single, Comments: commentsBetween(source, start, end)})
}

// renderValueSpec prints the requested name of one const or var spec. A const
// spec that repeats the previous expression implicitly is printed with that
// type and expression, and a const value that uses iota names the iota value
// it takes. Other names of the spec are left out, or printed as _ when they
// share one multi-value expression, and so are the comments of a spec with
// several names other than its doc comment and comments inside the kept parts.
func renderValueSpec(fset *token.FileSet, source contractSource, decl *ast.GenDecl, index int, symbol string) (string, error) {
	spec := decl.Specs[index].(*ast.ValueSpec)
	doc := specDoc(decl, spec.Doc)
	start, end := spec.Pos(), spec.End()
	if doc != nil {
		start = doc.Pos()
	}
	if spec.Comment != nil {
		end = spec.Comment.End()
	}
	copied := *spec
	copied.Doc = nil
	commentRanges := [][2]token.Pos{{start, end}}
	implicit := false
	if decl.Tok == token.CONST && len(copied.Values) == 0 {
		for previous := index - 1; previous >= 0; previous-- {
			if earlier := decl.Specs[previous].(*ast.ValueSpec); len(earlier.Values) > 0 {
				copied.Type, copied.Values, implicit = earlier.Type, earlier.Values, true
				break
			}
		}
	}
	if len(copied.Names) > 1 {
		at := slices.IndexFunc(copied.Names, func(name *ast.Ident) bool { return name.Name == symbol })
		// Keep only comments inside the kept name, type and values (the
		// printer emits the doc comment from the declaration itself). The line
		// comment is dropped: the parser groups every comment after the last
		// value into it, including another name's.
		commentRanges = [][2]token.Pos{{copied.Names[at].Pos(), copied.Names[at].End()}}
		copied.Comment = nil
		if spec.Type != nil {
			commentRanges = append(commentRanges, [2]token.Pos{spec.Type.Pos(), spec.Type.End()})
		}
		for i, value := range spec.Values {
			if len(spec.Values) != len(spec.Names) || i == at {
				commentRanges = append(commentRanges, [2]token.Pos{value.Pos(), value.End()})
			}
		}
		switch len(copied.Values) {
		case 0:
			copied.Names = copied.Names[at : at+1]
		case len(copied.Names):
			copied.Names, copied.Values = copied.Names[at:at+1], copied.Values[at:at+1]
		default:
			names := make([]*ast.Ident, len(copied.Names))
			for i, name := range copied.Names {
				names[i] = name
				if i != at {
					names[i] = &ast.Ident{NamePos: name.NamePos, Name: "_"}
				}
			}
			copied.Names = names
		}
	}
	usesIota := false
	if decl.Tok == token.CONST {
		for _, value := range copied.Values {
			ast.Inspect(value, func(n ast.Node) bool {
				if ident, ok := n.(*ast.Ident); ok && ident.Name == "iota" {
					usesIota = true
				}
				return !usesIota
			})
		}
	}
	if implicit {
		// The inherited type and values come from an earlier line; print them
		// apart so the printer does not merge positions from two specs.
		var line strings.Builder
		if doc != nil {
			for _, comment := range doc.List {
				line.WriteString(comment.Text + "\n")
			}
		}
		names := []string{}
		for _, name := range copied.Names {
			names = append(names, name.Name)
		}
		line.WriteString("const " + strings.Join(names, ", "))
		if copied.Type != nil {
			text, err := printNode(fset, copied.Type)
			if err != nil {
				return "", err
			}
			line.WriteString(" " + text)
		}
		values := []string{}
		for _, value := range copied.Values {
			text, err := printNode(fset, value)
			if err != nil {
				return "", err
			}
			values = append(values, text)
		}
		line.WriteString(" = " + strings.Join(values, ", "))
		if spec.Comment != nil && len(spec.Names) == 1 {
			line.WriteString(" " + strings.Join(commentTexts(spec.Comment), " "))
		}
		if usesIota {
			line.WriteString("\n// iota = " + strconv.Itoa(index))
		}
		return line.String(), nil
	}
	single := &ast.GenDecl{Doc: doc, TokPos: spec.Pos(), Tok: decl.Tok, Specs: []ast.Spec{&copied}}
	text, err := printNode(fset, &printer.CommentedNode{Node: single, Comments: commentsWithin(source, commentRanges)})
	if err != nil {
		return "", err
	}
	if usesIota {
		text += "\n// iota = " + strconv.Itoa(index)
	}
	return text, nil
}

func commentTexts(group *ast.CommentGroup) []string {
	texts := []string{}
	for _, comment := range group.List {
		texts = append(texts, comment.Text)
	}
	return texts
}

// storeContract checks a test-engineer brief's contract extract and stores an
// immutable copy in the task directory. The extract must be saved outside the
// project source, describe a package inside it, and equal, byte for byte, what
// ExtractContract yields for the current source.
func storeContract(root *os.Root, record Record, brief WorkerBrief) (string, error) {
	resolved := brief.Contract.Path
	if parent, err := filepath.EvalSymlinks(filepath.Dir(resolved)); err == nil {
		resolved = filepath.Join(parent, filepath.Base(resolved))
	}
	if within(record.Baseline.Root, resolved) {
		return "", errors.New("save the contract extract outside the project source")
	}
	data, err := readInputFile("contract extract", brief.Contract.Path)
	if err != nil {
		return "", err
	}
	if digest(data) != brief.Contract.SHA256 {
		return "", errors.New("contract extract sha256 differs from the file's bytes")
	}
	var saved Contract
	if err := decodeBytes(data, &saved); err != nil {
		return "", fmt.Errorf("contract extract: %w", err)
	}
	if saved.Language != "go" || !filepath.IsAbs(saved.Root) {
		return "", errors.New("contract extract must be cw task contract-extract output (Go only)")
	}
	dir := filepath.Join(saved.Root, filepath.FromSlash(saved.Dir))
	if !within(record.Baseline.Root, dir) {
		return "", errors.New("contract extract describes a package outside the task's project")
	}
	current, err := ExtractContract(dir, saved.Symbols)
	if err != nil {
		return "", fmt.Errorf("contract extract no longer matches the current source: %w; rerun cw task contract-extract", err)
	}
	expected, err := EncodeContract(current)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(expected, data) {
		return "", errors.New("contract extract differs from what cw task contract-extract prints for the current source (a source edit since extraction, or an edited extract); rerun it")
	}
	id, err := randomID()
	if err != nil {
		return "", err
	}
	name := "contract-" + brief.ID + "-" + id + ".json"
	if err := writeNew(root, name, data); err != nil {
		return "", err
	}
	return name, nil
}

func readStoredContract(root *os.Root, assigned WorkerAssignment) (Contract, error) {
	if assigned.ContractFile == "" || assigned.Brief.Contract == nil {
		return Contract{}, errors.New("assignment has no stored contract")
	}
	data, err := privateRead(root, assigned.ContractFile, MaxInputBytes)
	if err != nil {
		return Contract{}, err
	}
	if digest(data) != assigned.Brief.Contract.SHA256 {
		return Contract{}, errors.New("stored contract extract hash differs")
	}
	var contract Contract
	if err := decodeBytes(data, &contract); err != nil {
		return Contract{}, err
	}
	return contract, nil
}

// contractSuperseded reports whether a later contract-bearing assignment owns
// every path the assignment at index owns: a test-engineer correction round with
// a refreshed contract replaces the earlier contract. Only a round with an
// accepted acceptance and no rejected one supersedes (R18).
func contractSuperseded(record Record, index int) bool {
	owned := record.Assignments[index].Brief.OwnedPaths
	for _, later := range record.Assignments[index+1:] {
		if later.ContractFile == "" {
			continue
		}
		if !hasAcceptance(record, later.Brief.ID, "accepted") || hasAcceptance(record, later.Brief.ID, "rejected") {
			continue
		}
		if !slices.ContainsFunc(owned, func(path string) bool {
			return !slices.ContainsFunc(later.Brief.OwnedPaths, func(scope string) bool { return scopeContains(scope, path) })
		}) {
			return true
		}
	}
	return false
}

// contractProblems re-extracts every current contract. A changed, added or
// missing declaration is a problem: the tests were authored against a contract
// the source no longer has. Source files whose bytes changed while every
// declaration stayed the same are only noted.
func contractProblems(root *os.Root, record Record) (problems, notes []string) {
	for index, assigned := range record.Assignments {
		if assigned.ContractFile == "" {
			continue
		}
		saved, err := readStoredContract(root, assigned)
		if err != nil {
			problems = append(problems, fmt.Sprintf("worker assignment %s contract: %v", assigned.Brief.ID, err))
			continue
		}
		if contractSuperseded(record, index) {
			continue
		}
		current, err := ExtractContract(filepath.Join(saved.Root, filepath.FromSlash(saved.Dir)), saved.Symbols)
		if err != nil {
			problems = append(problems, fmt.Sprintf("worker assignment %s contract is stale: %v; assign a test-engineer correction round with a fresh contract-extract", assigned.Brief.ID, err))
			continue
		}
		if changed := changedContractSymbols(saved, current); len(changed) > 0 {
			problems = append(problems, fmt.Sprintf("worker assignment %s contract is stale: the declarations of %s changed since extraction; assign a test-engineer correction round with a fresh contract-extract", assigned.Brief.ID, strings.Join(changed, ", ")))
			continue
		}
		if changed := changedContractFiles(saved, current); len(changed) > 0 {
			notes = append(notes, fmt.Sprintf("worker assignment %s contract: declarations unchanged, but %s changed since extraction", assigned.Brief.ID, strings.Join(changed, ", ")))
		}
	}
	return problems, notes
}

// changedContractSymbols lists, in contract order, the symbols whose
// declarations (kind and text, wherever they are declared) differ; a moved
// declaration or a shifted line is not a change.
func changedContractSymbols(saved, current Contract) []string {
	texts := func(contract Contract, symbol string) []string {
		values := []string{}
		for _, declaration := range contract.Declarations {
			if declaration.Symbol == symbol {
				values = append(values, declaration.Kind+"\x00"+declaration.Text)
			}
		}
		slices.Sort(values)
		return values
	}
	changed := []string{}
	for _, symbol := range saved.Symbols {
		if !slices.Equal(texts(saved, symbol), texts(current, symbol)) {
			changed = append(changed, symbol)
		}
	}
	return changed
}

func changedContractFiles(saved, current Contract) []string {
	hashes := map[string]string{}
	for _, file := range current.Files {
		hashes[file.Path] = file.SHA256
	}
	changed := []string{}
	for _, file := range saved.Files {
		if hashes[file.Path] != file.SHA256 {
			changed = append(changed, file.Path)
		}
	}
	return changed
}
