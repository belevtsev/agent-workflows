package tokens

import (
	"cmp"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
)

// Amount is a number of tokens, or unknown. An unknown amount is never
// printed as zero.
type Amount struct {
	N       int64
	Unknown bool
}

// MarshalJSON writes the number, or the word unknown.
func (a Amount) MarshalJSON() ([]byte, error) {
	if a.Unknown {
		return []byte(`"unknown"`), nil
	}
	return strconv.AppendInt(nil, a.N, 10), nil
}

// Entry sums the starts of one role, of one stage and round, or of one
// session.
type Entry struct {
	Role  string `json:"role,omitempty"`
	Stage string `json:"stage,omitempty"`
	Round int    `json:"round,omitempty"`
	// Starts counts the starts; UnknownStarts those of them whose tokens
	// are unknown. With one of those the sums are at least what is shown.
	Starts        int `json:"starts"`
	UnknownStarts int `json:"unknown_starts,omitempty"`
	Counts
	// Total is unknown when the tokens of no start are known.
	Total  Amount   `json:"total"`
	Models []string `json:"models"`
}

func (e *Entry) add(s Start) {
	e.Starts++
	if e.Models == nil {
		e.Models = []string{}
	}
	if !s.Known {
		e.UnknownStarts++
	}
	for model, counts := range s.Models {
		e.Counts.add(counts)
		if !slices.Contains(e.Models, model) {
			e.Models = append(e.Models, model)
		}
	}
	slices.Sort(e.Models)
	e.Total = Amount{N: e.Sum(), Unknown: e.UnknownStarts == e.Starts}
}

// Flow holds the tokens that went to the workflow and not to the first
// writing of the specification, the design, the tests and the code.
type Flow struct {
	Total        int64 `json:"total"`
	Coordination int64 `json:"coordination"` // the session itself
	Review       int64 `json:"review"`
	Lookup       int64 `json:"lookup"`
	Git          int64 `json:"git"`
	Rework       int64 `json:"rework"` // a later round of a writing role
}

// Task is the report of one task.
type Task struct {
	Task string `json:"task"`
	// Total is the sum of every start whose tokens are known. Complete says
	// that it leaves no start out.
	Total    int64 `json:"total"`
	Complete bool  `json:"complete"`
	Starts   int   `json:"starts"`
	Writing  int64 `json:"writing"`
	Flow     Flow  `json:"flow"`
	// FlowShare is the flow tokens as a share of the total, in per cent.
	FlowShare int     `json:"flow_share"`
	Roles     []Entry `json:"roles"`
	Stages    []Entry `json:"stages"`
	// UnknownRecords names the records whose tokens are unknown.
	UnknownRecords []string `json:"unknown_records,omitempty"`
	// SharedSessions names the sessions that also ran another task. Their
	// own tokens are in the total of no task.
	SharedSessions []string `json:"shared_sessions,omitempty"`
	// unknownSessions counts the unknown records that are a session's own.
	unknownSessions int
}

// Unknown says how many of the unknown records are starts of roles, or
// records that may be, and how many are a session's own record.
func (t *Task) Unknown() (starts, sessions int) {
	return len(t.UnknownRecords) - t.unknownSessions, t.unknownSessions
}

// Session is the report of the tokens of one session itself.
type Session struct {
	Session string `json:"session"`
	Host    string `json:"host"`
	Record  string `json:"record,omitempty"`
	// Tasks names the tasks the session ran. With more than one, the
	// tokens of the session are in the total of none of them.
	Tasks  []string `json:"tasks"`
	Shared bool     `json:"shared"`
	Entry
}

// Unlabelled is a start whose record gave no label. When its tokens are
// known they belong to no task and are not dropped. When they are unknown
// the start may belong to any task of its session, and each of those tasks
// is incomplete.
type Unlabelled struct {
	Session string `json:"session"`
	Agent   string `json:"agent"`
	Record  string `json:"record"`
	Entry
}

// Report is what `gatecub tokens` prints.
type Report struct {
	Tasks      []Task       `json:"tasks"`
	Unlabelled []Unlabelled `json:"unlabelled"`
	Sessions   []Session    `json:"sessions"`
	// Damaged names the stored files that cannot be read. Their tokens are
	// in no sum, and every task of their session is incomplete.
	Damaged []string `json:"damaged,omitempty"`
}

// the stages whose first round is the writing itself
var writing = []string{"spec", "design", "tests", "code"}

// part is the place of a labelled start in the split of a task.
func (f *Flow) part(s Start) *int64 {
	switch role := RoleOf(s.Stage); {
	case role == "reviewer":
		return &f.Review
	case role == "lookup":
		return &f.Lookup
	case role == "git":
		return &f.Git
	case slices.Contains(writing, s.Stage) && s.Round > 1:
		return &f.Rework
	}
	return nil
}

func sum(s Start) int64 {
	var total int64
	for _, counts := range s.Models {
		total += counts.Sum()
	}
	return total
}

// order is the place of a stage among the stages.
func order(stage string) int {
	return slices.IndexFunc(stageRoles, func(s struct{ stage, role string }) bool { return s.stage == stage })
}

// Build makes the report from the stored starts. damaged names the stored
// files that could not be read; each is in the folder of its session.
func Build(starts []Start, damaged []string) *Report {
	report := &Report{Tasks: []Task{}, Unlabelled: []Unlabelled{}, Sessions: []Session{}, Damaged: damaged}
	byTask, tasksOf := group(starts)
	left := report.place(starts, byTask, tasksOf)
	for _, file := range damaged {
		folder := filepath.Base(filepath.Dir(file))
		for session, tasks := range tasksOf {
			if fileName(session) != folder {
				continue
			}
			for _, task := range tasks {
				left[task] = append(left[task], file)
			}
		}
	}
	sessions := report.sessions(starts, tasksOf)
	for _, name := range slices.Sorted(maps.Keys(byTask)) {
		task := buildTask(name, byTask[name], tasksOf, sessions)
		if len(left[name]) > 0 {
			task.Complete = false
			task.UnknownRecords = append(task.UnknownRecords, left[name]...)
		}
		report.Tasks = append(report.Tasks, task)
	}
	return report
}

// group sorts the starts that have a label by their task, and lists for
// each session the tasks it ran.
func group(starts []Start) (byTask map[string][]Start, tasksOf map[string][]string) {
	byTask, tasksOf = map[string][]Start{}, map[string][]string{}
	for _, s := range starts {
		if s.Agent == SessionAgent || s.Task == "" {
			continue
		}
		byTask[s.Task] = append(byTask[s.Task], s)
		if !slices.Contains(tasksOf[s.Session], s.Task) {
			tasksOf[s.Session] = append(tasksOf[s.Session], s.Task)
		}
	}
	return byTask, tasksOf
}

// place gives each start without a label its place. With known tokens it
// belongs to no task. With unknown tokens the label could not be read
// either: the start is given to the one task of its session when the host
// named its role, and otherwise it may belong to any task of its session.
// left lists, for each task, the records that may hold tokens of it.
func (r *Report) place(starts []Start, byTask map[string][]Start, tasksOf map[string][]string) (left map[string][]string) {
	left = map[string][]string{}
	for _, s := range starts {
		if s.Agent == SessionAgent || s.Task != "" {
			continue
		}
		tasks := tasksOf[s.Session]
		if !s.Known && s.Role != "" && len(tasks) == 1 {
			byTask[tasks[0]] = append(byTask[tasks[0]], s)
			continue
		}
		u := Unlabelled{Session: s.Session, Agent: s.Agent, Record: s.Record}
		u.Role = s.Role
		u.add(s)
		r.Unlabelled = append(r.Unlabelled, u)
		if !s.Known {
			for _, task := range tasks {
				left[task] = append(left[task], s.Record)
			}
		}
	}
	return left
}

// sessions adds the entry of each session to the report and returns the
// start of each.
func (r *Report) sessions(starts []Start, tasksOf map[string][]string) map[string]Start {
	sessions := map[string]Start{}
	for _, s := range starts {
		if s.Agent != SessionAgent {
			continue
		}
		sessions[s.Session] = s
		tasks := slices.Sorted(slices.Values(tasksOf[s.Session]))
		if tasks == nil {
			tasks = []string{}
		}
		entry := Session{Session: s.Session, Host: s.Host, Record: s.Record, Tasks: tasks, Shared: len(tasks) > 1}
		entry.add(s)
		r.Sessions = append(r.Sessions, entry)
	}
	return sessions
}

func buildTask(name string, starts []Start, tasksOf map[string][]string, sessions map[string]Start) Task {
	task := Task{Task: name, Complete: true}
	roles, stages := map[string]*Entry{}, map[[2]string]*Entry{}
	count := func(s Start, part *int64) {
		if !s.Known {
			task.Complete = false
			task.UnknownRecords = append(task.UnknownRecords, s.Record)
			return
		}
		n := sum(s)
		task.Total += n
		if part == nil {
			task.Writing += n
			return
		}
		*part += n
		task.Flow.Total += n
	}
	var seen []string
	for _, s := range starts {
		// A start without a stage is one whose record could not be read:
		// the host named its role, and it is in no stage.
		role := cmp.Or(RoleOf(s.Stage), s.Role)
		if roles[role] == nil {
			roles[role] = &Entry{Role: role}
		}
		roles[role].add(s)
		key := [2]string{s.Stage, strconv.Itoa(s.Round)}
		if s.Stage != "" {
			if stages[key] == nil {
				stages[key] = &Entry{Stage: s.Stage, Round: s.Round, Role: role}
			}
			stages[key].add(s)
		}
		task.Starts++
		count(s, task.Flow.part(s))
		if !slices.Contains(seen, s.Session) {
			seen = append(seen, s.Session)
		}
	}
	// The session itself: its tokens are the coordination of the one task
	// it ran. A session that ran several gives them to none.
	slices.Sort(seen)
	for _, id := range seen {
		session, recorded := sessions[id]
		switch {
		case len(tasksOf[id]) > 1:
			task.SharedSessions = append(task.SharedSessions, id)
		case !recorded:
			task.Complete = false
			task.UnknownRecords = append(task.UnknownRecords, "the record of session "+id)
			task.unknownSessions++
		default:
			if !session.Known {
				task.unknownSessions++
			}
			count(session, &task.Flow.Coordination)
		}
	}
	if task.Total > 0 {
		task.FlowShare = int((task.Flow.Total*100 + task.Total/2) / task.Total)
	}
	for _, entry := range roles {
		task.Roles = append(task.Roles, *entry)
	}
	slices.SortFunc(task.Roles, func(a, b Entry) int {
		return cmp.Compare(slices.Index(roleOrder(), a.Role), slices.Index(roleOrder(), b.Role))
	})
	for _, entry := range stages {
		task.Stages = append(task.Stages, *entry)
	}
	slices.SortFunc(task.Stages, func(a, b Entry) int {
		return cmp.Or(cmp.Compare(order(a.Stage), order(b.Stage)), cmp.Compare(a.Round, b.Round))
	})
	return task
}

// roleOrder lists the roles in the order of their first stage.
func roleOrder() []string {
	var roles []string
	for _, s := range stageRoles {
		if !slices.Contains(roles, s.role) {
			roles = append(roles, s.role)
		}
	}
	return roles
}

// Find returns the report of one task with the sessions that ran it and
// their starts without a label, and false when no start names the task.
func (r *Report) Find(task string) (*Task, []Session, []Unlabelled, bool) {
	at := slices.IndexFunc(r.Tasks, func(t Task) bool { return t.Task == task })
	if at < 0 {
		return nil, nil, nil, false
	}
	sessions, unlabelled := []Session{}, []Unlabelled{}
	for _, s := range r.Sessions {
		if slices.Contains(s.Tasks, task) {
			sessions = append(sessions, s)
		}
	}
	for _, u := range r.Unlabelled {
		if slices.ContainsFunc(sessions, func(s Session) bool { return s.Session == u.Session }) {
			unlabelled = append(unlabelled, u)
		}
	}
	return &r.Tasks[at], sessions, unlabelled, true
}
