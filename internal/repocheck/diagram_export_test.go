package repocheck

import (
	"bytes"
	"cmp"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// boxTypes are the types that archify gives a box. Its legend has one entry
// for each of them, and a box without an icon of its own shows the icon of
// its type.
var boxTypes = []string{"frontend", "backend", "database", "cloud", "security", "messagebus", "external"}

// diagramSpec is the part of a spec that its export records. A workflow
// names its boxes nodes and its arrows edges; an architecture names them
// components and connections.
type diagramSpec struct {
	Meta struct {
		Title   string `json:"title"`
		Quality string `json:"quality_profile"`
		Legend  struct {
			Mode    string                   `json:"mode"`
			Entries map[string]diagramLegend `json:"entries"`
		} `json:"legend"`
	} `json:"meta"`
	Lanes       []diagramText  `json:"lanes"`
	Phases      []diagramText  `json:"phases"`
	Groups      []diagramText  `json:"groups"`
	Boundaries  []diagramText  `json:"boundaries"`
	Nodes       []diagramBox   `json:"nodes"`
	Components  []diagramBox   `json:"components"`
	Edges       []diagramArrow `json:"edges"`
	Connections []diagramArrow `json:"connections"`
}

// diagramLegend is the entry of one type of box in the legend.
type diagramLegend struct {
	Label   string `json:"label"`
	Visible *bool  `json:"visible"`
}

// diagramText is a lane, a phase, a group or a boundary. A phase and a
// group span columns, a group in one lane, and a boundary wraps boxes.
type diagramText struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Variant string   `json:"variant"`
	Kind    string   `json:"kind"`
	Lane    string   `json:"lane"`
	FromCol int      `json:"fromCol"`
	ToCol   int      `json:"toCol"`
	Wraps   []string `json:"wraps"`
}

func (t diagramText) spans(box diagramBox) bool {
	return box.Col >= t.FromCol && box.Col <= t.ToCol
}

type diagramBox struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Icon     string `json:"icon"`
	Label    string `json:"label"`
	Sublabel string `json:"sublabel"`
	Tag      string `json:"tag"`
	Lane     string `json:"lane"`
	Col      int    `json:"col"`
	// Scope is where the box stands: the labels of its lane, its group and
	// its phase, or of the boundaries that wrap it, in the order of the
	// alphabet.
	Scope string `json:"-"`
}

func (b diagramBox) String() string {
	line := fmt.Sprintf("the box %s (%s, %s) %q", b.ID, b.Type, cmp.Or(b.Icon, "no")+" icon", b.Label)
	if b.Sublabel != "" {
		line += fmt.Sprintf(", sublabel %q", b.Sublabel)
	}
	if b.Tag != "" {
		line += fmt.Sprintf(", tag %q", b.Tag)
	}
	if b.Scope != "" {
		line += ", in " + b.Scope
	}
	return line
}

type diagramArrow struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Label   string `json:"label"`
	Variant string `json:"variant"`
	Role    string `json:"role"`
}

func (a diagramArrow) String() string {
	line := fmt.Sprintf("the arrow %s -> %s (%s", a.From, a.To, a.Variant)
	if a.Role != "" {
		line += ", " + a.Role
	}
	line += ")"
	if a.Label != "" {
		line += fmt.Sprintf(" %q", a.Label)
	}
	return line
}

// scopeSeparator stands between the parts of a box's scope in an export.
const scopeSeparator = " › "

func scope(labels ...string) string {
	labels = slices.DeleteFunc(labels, func(label string) bool { return label == "" })
	slices.Sort(labels)
	return strings.Join(labels, scopeSeparator)
}

// boxes returns the boxes of the spec as the export records them, by id.
func (d diagramSpec) boxes() map[string]diagramBox {
	boxes := map[string]diagramBox{}
	for _, box := range d.Nodes {
		var labels []string
		for _, lane := range d.Lanes {
			if lane.ID == box.Lane {
				labels = append(labels, lane.Label)
			}
		}
		if at := slices.IndexFunc(d.Groups, func(group diagramText) bool { return group.Lane == box.Lane && group.spans(box) }); at >= 0 {
			labels = append(labels, d.Groups[at].Label)
		}
		if at := slices.IndexFunc(d.Phases, func(phase diagramText) bool { return phase.spans(box) }); at >= 0 {
			labels = append(labels, d.Phases[at].Label)
		}
		box.Scope = scope(labels...)
		boxes[box.ID] = box
	}
	for _, box := range d.Components {
		var labels []string
		for _, boundary := range d.Boundaries {
			if slices.Contains(boundary.Wraps, box.ID) {
				labels = append(labels, boundary.Label)
			}
		}
		box.Scope = scope(labels...)
		boxes[box.ID] = box
	}
	for id, box := range boxes {
		// The export records the scope, not the lane and the column. A box
		// shows the icon of its type unless it names another, or "none".
		box.Lane, box.Col = "", 0
		if box.Icon = cmp.Or(box.Icon, box.Type); box.Icon == "none" {
			box.Icon = ""
		}
		boxes[id] = box
	}
	return boxes
}

// arrows returns the arrows of the spec, one line each. An arrow without a
// variant is drawn as a default one.
func (d diagramSpec) arrows() []string {
	var arrows []string
	for _, arrow := range slices.Concat(d.Edges, d.Connections) {
		arrow.Variant = cmp.Or(arrow.Variant, "default")
		arrows = append(arrows, arrow.String())
	}
	return arrows
}

// legend returns the label of each type that the legend of the export
// shows, and the types that it shows without a label of the spec, in
// archify's own words. archify shows a type that a box has, or every type
// in the mode "all"; an entry changes that with "visible"; and the mode
// "hidden" shows no legend.
func (d diagramSpec) legend() (labels map[string]string, unnamed []string) {
	labels = map[string]string{}
	mode := d.Meta.Legend.Mode
	if mode == "hidden" {
		return labels, nil
	}
	boxes := slices.Concat(d.Nodes, d.Components)
	for _, kind := range boxTypes {
		entry := d.Meta.Legend.Entries[kind]
		shown := mode == "all" || slices.ContainsFunc(boxes, func(box diagramBox) bool { return box.Type == kind })
		if entry.Visible != nil {
			shown = *entry.Visible
		}
		switch {
		case !shown:
		case entry.Label == "":
			unnamed = append(unnamed, kind)
		default:
			labels[kind] = entry.Label
		}
	}
	return labels, unnamed
}

// A mark is one thing that a picture shows outside its boxes and arrows:
// its title, a lane, a group, a phase, a boundary or a legend entry. It is
// one line, written the same from a spec and from its export.
type mark struct{ before, after string }

func (m mark) of(text string) string { return m.before + strconv.Quote(text) + m.after }

var (
	titleMark = mark{before: "the title "}
	laneMark  = mark{before: "the lane "}
	groupMark = mark{before: "the group "}
)

func phaseMark(variant string) mark  { return mark{"the phase ", " (" + variant + ")"} }
func boundaryMark(kind string) mark  { return mark{"the boundary ", " (" + kind + ")"} }
func legendMark(boxType string) mark { return mark{before: "the legend entry " + boxType + " "} }

// marks returns the marks of the spec. archify numbers the lanes, as in
// "01 / git", and writes EX in place of the number of a lane of exceptions.
func (d diagramSpec) marks() []string {
	marks := []string{titleMark.of(d.Meta.Title)}
	for at, lane := range d.Lanes {
		number := fmt.Sprintf("%02d", at+1)
		if lane.Variant == "exception" {
			number = "EX"
		}
		marks = append(marks, laneMark.of(number+" / "+lane.Label))
	}
	for _, group := range d.Groups {
		marks = append(marks, groupMark.of(group.Label))
	}
	for _, phase := range d.Phases {
		marks = append(marks, phaseMark(cmp.Or(phase.Variant, "default")).of(phase.Label))
	}
	for _, boundary := range d.Boundaries {
		marks = append(marks, boundaryMark(cmp.Or(boundary.Kind, "boundary")).of(boundary.Label))
	}
	labels, _ := d.legend()
	for _, kind := range boxTypes {
		if label, shown := labels[kind]; shown {
			marks = append(marks, legendMark(kind).of(label))
		}
	}
	return marks
}

// diagramExport is what an export records about itself: in the attributes
// of its tags, and in the text that follows the tag of a mark.
type diagramExport struct {
	quality string
	marks   []string
	boxes   map[string]diagramBox
	arrows  []string
}

func readExport(svg []byte) (diagramExport, error) {
	export := diagramExport{boxes: map[string]diagramBox{}}
	var (
		// awaited is the mark whose text comes next: archify writes a
		// title, a lane, a group, a phase and a legend entry as a tag, and
		// its label as the next text.
		awaited mark
		// box is the box whose tag is open, and boxDepth is how deep its
		// tag is.
		box             string
		depth, boxDepth int
	)
	decoder := xml.NewDecoder(bytes.NewReader(svg))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return export, nil
		}
		if err != nil {
			return export, err
		}
		switch token := token.(type) {
		case xml.CharData:
			if text := strings.TrimSpace(string(token)); text != "" && awaited != (mark{}) {
				export.marks = append(export.marks, awaited.of(text))
				awaited = mark{}
			}
		case xml.EndElement:
			if depth--; depth < boxDepth {
				box = ""
			}
		case xml.StartElement:
			depth++
			attributes := map[string]string{}
			for _, attribute := range token.Attr {
				attributes[attribute.Name.Local] = attribute.Value
			}
			// A phase and an arrow carry the class of their variant, such
			// as a-dashed.
			classes, variant := strings.Fields(attributes["class"]), ""
			for _, class := range classes {
				if name, found := strings.CutPrefix(class, "a-"); found {
					variant = name
				}
			}
			if quality := attributes["data-quality-profile"]; quality != "" {
				export.quality = quality
			}
			if id := attributes["data-node-id"]; id != "" {
				box, boxDepth = id, depth
				export.boxes[id] = diagramBox{
					ID: id, Type: attributes["data-node-kind"], Label: attributes["data-node-label"],
					Sublabel: attributes["data-node-sublabel"], Tag: attributes["data-node-tag"],
					Scope: scope(strings.Split(attributes["data-node-context"], scopeSeparator)...),
				}
			}
			if icon := attributes["data-semantic-sigil"]; icon != "" && box != "" {
				drawn := export.boxes[box]
				drawn.Icon = icon
				export.boxes[box] = drawn
			}
			// The label of an arrow is a group with the same ends and no
			// variant.
			if from := attributes["data-edge-from"]; from != "" && variant != "" {
				export.arrows = append(export.arrows, diagramArrow{
					From: from, To: attributes["data-edge-to"], Label: attributes["data-edge-label"],
					Variant: variant, Role: attributes["data-edge-role"],
				}.String())
			}
			frame, kind := attributes["data-graph-role"] == "structural-frame", attributes["data-composition-frame-kind"]
			label, labelled := attributes["data-composition-frame-label"]
			switch {
			case token.Name.Local == "title" && attributes["id"] == "archify-diagram-title":
				awaited = titleMark
			case frame && kind == "lane":
				awaited = laneMark
			case frame && kind == "group":
				awaited = groupMark
			case frame && labelled:
				export.marks = append(export.marks, boundaryMark(kind).of(label))
			case slices.Contains(classes, "phase-rule"):
				awaited = phaseMark(variant)
			case attributes["data-legend-semantic-kind"] != "":
				awaited = legendMark(attributes["data-legend-semantic-kind"])
			}
		}
	}
}

// exportProblems compares a spec with its export, to find an export that
// was not made again after the spec changed. Each must hold what the other
// holds, as often as the other holds it: the quality profile, the title,
// the lanes in their order, the groups, the phases with their variant, the
// boundaries with their kind, the legend entries with their type, every box
// with its type, its icon, its texts and its scope, and every arrow with
// its ends, its label, its variant and its role.
//
// For the legend to be compared, the spec must give a label to every type
// that its legend shows; archify's own word for a type is not in the spec.
//
// The spec holds more than an export records. This cannot see where a box
// stands inside its lane, its group and its phase, or inside its
// boundaries; the size of anything, or the columns that a group or a phase
// spans beside its boxes; the route of an arrow and the sides it leaves and
// enters by; or the variant of a group. After such a change only a look at
// the picture tells whether the export is the new one.
func exportProblems(root, spec, export string) []string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(spec)))
	if err != nil {
		return []string{err.Error()}
	}
	var diagram diagramSpec
	if err := json.Unmarshal(data, &diagram); err != nil {
		return []string{fmt.Sprintf("%s: %v", spec, err)}
	}
	svg, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(export)))
	if err != nil {
		return []string{err.Error()}
	}
	found, err := readExport(svg)
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", export, err)}
	}
	if _, unnamed := diagram.legend(); len(unnamed) > 0 {
		return []string{fmt.Sprintf("%s: its legend shows %s in archify's own words; give each type a label under meta.legend.entries",
			spec, strings.Join(unnamed, ", "))}
	}

	var lacks, extra []string
	if diagram.Meta.Quality != "" && found.quality != diagram.Meta.Quality {
		lacks = append(lacks, "the quality profile "+diagram.Meta.Quality)
	}
	marks := diagram.marks()
	lacks = append(lacks, missing(marks, found.marks)...)
	extra = append(extra, missing(found.marks, marks)...)
	want := diagram.boxes()
	for _, id := range slices.Sorted(maps.Keys(want)) {
		box, drawn := want[id], found.boxes[id]
		if box.Scope == "" {
			// archify gives a box without a lane or a boundary a scope of
			// its own words, which the spec does not hold.
			drawn.Scope = ""
		}
		if drawn != box {
			lacks = append(lacks, box.String())
			if drawn.ID != "" {
				extra = append(extra, drawn.String())
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(found.boxes)) {
		if _, wanted := want[id]; !wanted {
			extra = append(extra, found.boxes[id].String())
		}
	}
	arrows := diagram.arrows()
	lacks = append(lacks, missing(arrows, found.arrows)...)
	extra = append(extra, missing(found.arrows, arrows)...)

	var problems []string
	for _, what := range lacks {
		problems = append(problems, fmt.Sprintf("%s: it lacks %s of %s; export it again", export, what, spec))
	}
	for _, what := range extra {
		problems = append(problems, fmt.Sprintf("%s: it holds %s, which %s does not; export it again", export, what, spec))
	}
	return problems
}

// missing returns the lines of want that have does not hold. A line that
// want holds more often than have is missing for each time more.
func missing(want, have []string) []string {
	left := map[string]int{}
	for _, line := range have {
		left[line]++
	}
	var lacking []string
	for _, line := range want {
		if left[line] > 0 {
			left[line]--
			continue
		}
		lacking = append(lacking, line)
	}
	return lacking
}

func TestAStaleExportIsFound(t *testing.T) {
	const (
		specFile   = "docs/diagrams/a.json"
		exportFile = "docs/diagrams/a.svg"
		stale      = exportFile + ": it lacks %s of " + specFile + "; export it again"
		left       = exportFile + ": it holds %s, which " + specFile + " does not; export it again"

		secondNode = `{"id": "second", "lane": "work", "col": 1, "type": "database", "label": "second"}`
		bothEdges  = `[{"from": "first", "to": "second", "label": "then", "variant": "dashed", "role": "return"}, {"from": "second", "to": "first"}]`
		spec       = `{"meta": {"title": "Two steps", "quality_profile": "showcase",
			"legend": {"entries": {"backend": {"label": "A step"}, "database": {"label": "A store"}}}},
			"lanes": [{"id": "work", "label": "Work"}],
			"groups": [{"id": "both", "label": "Both", "lane": "work", "fromCol": 0, "toCol": 1}],
			"phases": [{"id": "start", "label": "Start", "fromCol": 0, "toCol": 0}],
			"nodes": [{"id": "first", "lane": "work", "col": 0, "type": "backend", "icon": "start", "label": "first", "sublabel": "a < b", "tag": "now"}, ` + secondNode + `],
			"edges": ` + bothEdges + `}`
		backPath = `<path data-edge-from="second" data-edge-to="first" data-edge-id="two" data-edge-role="" class="a-default" d="M 0 0"/>`
		export   = `<?xml version="1.0" encoding="UTF-8"?>
			<svg viewBox="0 0 10 10" data-quality-profile="showcase" xmlns="http://www.w3.org/2000/svg">
			<title id="archify-diagram-title">Two steps</title><desc id="archify-diagram-description">A workflow diagram.</desc>
			<rect data-graph-role="structural-frame" data-composition-frame-kind="lane" data-composition-frame-id="lane-0"/>
			<text>01 / Work</text>
			<path d="M 0 0" class="a-default phase-rule"/><rect class="c-mask phase-chip"/><text>Start</text>
			<rect data-graph-role="structural-frame" data-composition-frame-kind="group" data-composition-frame-id="group-0"/><text>Both</text>
			<path data-edge-from="first" data-edge-to="second" data-edge-label="then" data-edge-id="one" data-edge-role="return" class="a-dashed" d="M 0 0"/>
			` + backPath + `
			<g id="node-first" data-node-id="first" data-node-label="first" role="button" data-node-kind="backend"
				data-node-sublabel="a &lt; b" data-node-tag="now" data-node-context="Work › Both › Start">
				<title>first · a &lt; b</title><g data-semantic-sigil="start" class="semantic-sigil s-backend"><path d="M 0 0"/></g>
				<text data-node-label="">first</text><text>a &lt; b</text></g>
			<g id="node-second" data-node-id="second" data-node-label="second" data-node-kind="database" data-node-context="Work › Both">
				<g data-semantic-sigil="database"/><text data-node-label="">second</text></g>
			<g data-edge-from="first" data-edge-to="second" data-edge-label="then" data-edge-id="one"><text>then</text></g>
			<g data-legend-semantic-kind="backend"><rect class="c-backend"/><text>A step</text></g>
			<g data-legend-semantic-kind="database"><rect class="c-database"/><text>A store</text></g></svg>`

		inside = `{"kind": "region", "label": "Inside", "wraps": ["store"]}`
		all    = `{"kind": "security-group", "label": "All", "wraps": ["store"]}`
		parts  = `{"meta": {"title": "Two parts", "legend": {"mode": "hidden"}},
			"components": [{"id": "store", "type": "database", "label": "store"}, {"id": "alone", "type": "external", "icon": "none", "label": "alone"}],
			"boundaries": [` + inside + `, ` + all + `],
			"connections": [{"from": "alone", "to": "store", "variant": "emphasis"}]}`
		partsExport = `<svg xmlns="http://www.w3.org/2000/svg"><title id="archify-diagram-title">Two parts</title>
			<rect data-graph-role="structural-frame" data-composition-frame-kind="region" data-composition-frame-id="0" data-composition-frame-label="Inside"/>
			<rect data-graph-role="structural-frame" data-composition-frame-kind="security-group" data-composition-frame-id="1" data-composition-frame-label="All"/>
			<g data-graph-role="structural-frame-label" data-composition-frame-id="0" data-composition-frame-kind="region"
				data-composition-frame-label="Inside"><text data-boundary-label="">Inside</text></g>
			<path data-edge-from="alone" data-edge-to="store" class="a-emphasis" d="M 0 0"/>
			<g data-node-id="store" data-node-label="store" data-node-kind="database" data-node-context="Inside › All"><g data-semantic-sigil="database"/></g>
			<g data-node-id="alone" data-node-label="alone" data-node-kind="external" data-node-context="Architecture"></g></svg>`

		first      = `the box first (backend, start icon) "first", sublabel "a < b", tag "now", in Both › Start › Work`
		second     = `the box second (database, database icon) "second", in Both › Work`
		then       = `the arrow first -> second (dashed, return) "then"`
		back       = "the arrow second -> first (default)"
		stepEntry  = `the legend entry backend "A step"`
		storeEntry = `the legend entry database "A store"`
	)
	changed := func(text, old, replacement string) string {
		if !strings.Contains(text, old) {
			t.Fatalf("the fixture does not hold %q", old)
		}
		return strings.Replace(text, old, replacement, 1)
	}
	for _, test := range []struct {
		name         string
		spec, export string
		want         []string
	}{
		{name: "a workflow", spec: spec, export: export},
		{name: "an architecture", spec: parts, export: partsExport},
		{
			name: "a label that changed",
			spec: changed(spec, `"label": "second"`, `"label": "third"`), export: export,
			want: []string{
				fmt.Sprintf(stale, `the box second (database, database icon) "third", in Both › Work`),
				fmt.Sprintf(left, second),
			},
		},
		{
			name: "a title and a legend label that changed",
			spec: changed(changed(spec, "Two steps", "Steps"), "A step", "Any step"), export: export,
			want: []string{
				fmt.Sprintf(stale, `the title "Steps"`), fmt.Sprintf(stale, `the legend entry backend "Any step"`),
				fmt.Sprintf(left, `the title "Two steps"`), fmt.Sprintf(left, stepEntry),
			},
		},
		{
			name: "legend labels that changed places",
			spec: changed(changed(spec, `{"label": "A step"}`, `{"label": "A store"}`), `"database": {"label": "A store"}`, `"database": {"label": "A step"}`), export: export,
			want: []string{
				fmt.Sprintf(stale, `the legend entry backend "A store"`), fmt.Sprintf(stale, `the legend entry database "A step"`),
				fmt.Sprintf(left, stepEntry), fmt.Sprintf(left, storeEntry),
			},
		},
		{
			name: "a legend label that a box also shows",
			spec: changed(spec, "A step", "first"), export: export,
			want: []string{fmt.Sprintf(stale, `the legend entry backend "first"`), fmt.Sprintf(left, stepEntry)},
		},
		{
			name: "a legend entry that the spec no longer names",
			spec: changed(spec, `, "database": {"label": "A store"}`, ""), export: export,
			want: []string{specFile + ": its legend shows database in archify's own words; give each type a label under meta.legend.entries"},
		},
		{
			name: "a legend entry that the spec hides",
			spec: changed(spec, `{"label": "A store"}`, `{"label": "A store", "visible": false}`), export: export,
			want: []string{fmt.Sprintf(left, storeEntry)},
		},
		{
			name: "a legend that the spec hides",
			spec: changed(spec, `"legend": {"entries"`, `"legend": {"mode": "hidden", "entries"`), export: export,
			want: []string{fmt.Sprintf(left, stepEntry), fmt.Sprintf(left, storeEntry)},
		},
		{
			name: "a lane that changed its place",
			spec: changed(spec, `[{"id": "work", "label": "Work"}]`, `[{"id": "rest", "label": "Rest"}, {"id": "work", "label": "Work"}]`), export: export,
			want: []string{
				fmt.Sprintf(stale, `the lane "01 / Rest"`), fmt.Sprintf(stale, `the lane "02 / Work"`),
				fmt.Sprintf(left, `the lane "01 / Work"`),
			},
		},
		{
			name: "a lane of exceptions",
			spec: changed(spec, `{"id": "work", "label": "Work"}`, `{"id": "work", "label": "Work", "variant": "exception"}`), export: export,
			want: []string{fmt.Sprintf(stale, `the lane "EX / Work"`), fmt.Sprintf(left, `the lane "01 / Work"`)},
		},
		{
			name: "a group with another name",
			spec: changed(spec, `"label": "Both"`, `"label": "Each"`), export: export,
			want: []string{
				fmt.Sprintf(stale, `the group "Each"`),
				fmt.Sprintf(stale, `the box first (backend, start icon) "first", sublabel "a < b", tag "now", in Each › Start › Work`),
				fmt.Sprintf(stale, `the box second (database, database icon) "second", in Each › Work`),
				fmt.Sprintf(left, `the group "Both"`), fmt.Sprintf(left, first), fmt.Sprintf(left, second),
			},
		},
		{
			name: "a phase of another variant",
			spec: changed(spec, `"label": "Start"`, `"label": "Start", "variant": "emphasis"`), export: export,
			want: []string{fmt.Sprintf(stale, `the phase "Start" (emphasis)`), fmt.Sprintf(left, `the phase "Start" (default)`)},
		},
		{
			name: "a box in another phase",
			spec: changed(spec, `"toCol": 0`, `"toCol": 1`), export: export,
			want: []string{
				fmt.Sprintf(stale, `the box second (database, database icon) "second", in Both › Start › Work`),
				fmt.Sprintf(left, second),
			},
		},
		{
			name: "a box of another type",
			spec: changed(spec, `"col": 1, "type": "database"`, `"col": 1, "type": "backend"`), export: export,
			want: []string{
				fmt.Sprintf(stale, `the box second (backend, backend icon) "second", in Both › Work`),
				fmt.Sprintf(left, storeEntry), fmt.Sprintf(left, second),
			},
		},
		{
			name: "a box with another icon",
			spec: changed(spec, `"icon": "start"`, `"icon": "stop"`), export: export,
			want: []string{
				fmt.Sprintf(stale, `the box first (backend, stop icon) "first", sublabel "a < b", tag "now", in Both › Start › Work`),
				fmt.Sprintf(left, first),
			},
		},
		{
			name: "a box without its icon",
			spec: changed(spec, `"type": "database", "label"`, `"type": "database", "icon": "none", "label"`), export: export,
			want: []string{
				fmt.Sprintf(stale, `the box second (database, no icon) "second", in Both › Work`),
				fmt.Sprintf(left, second),
			},
		},
		{
			name: "a box and its arrows that were removed",
			spec: changed(changed(spec, ", "+secondNode, ""), bothEdges, "[]"), export: export,
			want: []string{fmt.Sprintf(left, storeEntry), fmt.Sprintf(left, second), fmt.Sprintf(left, then), fmt.Sprintf(left, back)},
		},
		{
			name: "an arrow with other ends",
			spec: changed(spec, `{"from": "second", "to": "first"}`, `{"from": "first", "to": "first"}`), export: export,
			want: []string{fmt.Sprintf(stale, "the arrow first -> first (default)"), fmt.Sprintf(left, back)},
		},
		{
			name: "an arrow of another variant",
			spec: changed(spec, `"variant": "dashed"`, `"variant": "security"`), export: export,
			want: []string{fmt.Sprintf(stale, `the arrow first -> second (security, return) "then"`), fmt.Sprintf(left, then)},
		},
		{
			name: "an arrow in another role",
			spec: changed(spec, `"role": "return"`, `"role": "error"`), export: export,
			want: []string{fmt.Sprintf(stale, `the arrow first -> second (dashed, error) "then"`), fmt.Sprintf(left, then)},
		},
		{
			name: "an arrow that the spec holds twice",
			spec: changed(spec, `{"from": "second", "to": "first"}`, `{"from": "second", "to": "first"}, {"from": "second", "to": "first"}`), export: export,
			want: []string{fmt.Sprintf(stale, back)},
		},
		{
			name: "an arrow that the export draws twice",
			spec: spec, export: changed(export, backPath, backPath+backPath),
			want: []string{fmt.Sprintf(left, back)},
		},
		{
			name: "an export of another quality",
			spec: spec, export: changed(export, `data-quality-profile="showcase"`, `data-quality-profile="standard"`),
			want: []string{fmt.Sprintf(stale, "the quality profile showcase")},
		},
		{
			name: "an export that is not XML",
			spec: spec, export: "<svg><g></svg>",
			want: []string{exportFile + ": XML syntax error on line 1: element <g> closed by </svg>"},
		},
		{
			name: "a box outside its boundary",
			spec: changed(parts, all, `{"kind": "security-group", "label": "All", "wraps": ["alone"]}`), export: partsExport,
			want: []string{
				fmt.Sprintf(stale, `the box alone (external, no icon) "alone", in All`),
				fmt.Sprintf(stale, `the box store (database, database icon) "store", in Inside`),
				fmt.Sprintf(left, `the box alone (external, no icon) "alone", in Architecture`),
				fmt.Sprintf(left, `the box store (database, database icon) "store", in All › Inside`),
			},
		},
		{
			name: "a boundary of another kind",
			spec: changed(parts, inside, `{"kind": "security-group", "label": "Inside", "wraps": ["store"]}`), export: partsExport,
			want: []string{fmt.Sprintf(stale, `the boundary "Inside" (security-group)`), fmt.Sprintf(left, `the boundary "Inside" (region)`)},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeFixture(t, filepath.Join(root, filepath.FromSlash(specFile)), []byte(test.spec))
			writeFixture(t, filepath.Join(root, filepath.FromSlash(exportFile)), []byte(test.export))
			if got := exportProblems(root, specFile, exportFile); !slices.Equal(got, test.want) {
				t.Errorf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(test.want, "\n"))
			}
		})
	}
}
