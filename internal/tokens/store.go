package tokens

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/roles"
)

// SessionAgent is the agent of the start that is the session itself.
const SessionAgent = "session"

// Start is what kitsu keeps of one start: a role that ran, or the session
// itself. It holds counts and names, and no text of a brief or an answer.
type Start struct {
	Version int    `json:"version"`
	Host    string `json:"host"`
	Session string `json:"session"`
	Agent   string `json:"agent"`
	// Record is the file of the host the counts were read from, and Size
	// its size before that reading.
	Record string `json:"record"`
	Size   int64  `json:"size,omitempty"`
	Task   string `json:"task,omitempty"`
	Stage  string `json:"stage,omitempty"`
	Round  int    `json:"round,omitempty"`
	// Role is the role the host named in its notice. It is kept only when
	// the record gives no label, so that a start whose record cannot be
	// read still has its role.
	Role string `json:"role,omitempty"`
	// Known is false when the counts could not be read; Why says why.
	Known  bool              `json:"known"`
	Why    string            `json:"why,omitempty"`
	Models map[string]Counts `json:"models,omitempty"`
}

// Notice is what a host gives its hook when a role or a session ends.
type Notice struct {
	Session         string `json:"session_id"`
	Transcript      string `json:"transcript_path"`
	AgentID         string `json:"agent_id"`
	AgentTranscript string `json:"agent_transcript_path"`
	// AgentType is the name of the role as the host knows it. A plugin's
	// role has the plugin's name and a colon before its own.
	AgentType string `json:"agent_type"`
	// Cwd is the directory the host works in.
	Cwd string `json:"cwd"`
}

// Dir is the directory the repository is found from: the notice's cwd, or
// the working directory when the notice gives none.
func (n Notice) Dir() (string, error) {
	if n.Cwd != "" {
		return n.Cwd, nil
	}
	return os.Getwd()
}

// noticeRole is the role a notice names, or empty when it names none of the
// roles of kitsu.
func noticeRole(agentType string) string {
	name := agentType[strings.LastIndexByte(agentType, ':')+1:]
	if roles.Known(name) {
		return name
	}
	return ""
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// fileName is a name that is safe as a file name: the name itself, or a
// digest of it.
func fileName(name string) string {
	if safeName.MatchString(name) {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return "x" + hex.EncodeToString(sum[:8])
}

// stem is a file name without its directory and its extension.
func stem(path string) string {
	base := filepath.Base(filepath.FromSlash(path))
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// start makes what is stored of one start. role is the role the notice
// names; it is kept when the record gives no label.
func start(host, session, agent, record, role string, r reading) Start {
	s := Start{Version: 1, Host: host, Session: session, Agent: agent, Record: record, Size: r.size, Known: r.why == "", Why: r.why, Models: r.models}
	if agent == SessionAgent {
		return s
	}
	if label, ok := ParseLabel(r.brief); ok {
		s.Task, s.Stage, s.Round = label.Task, label.Stage, label.Round
	} else {
		s.Role = role
	}
	return s
}

// readers are the readers of the records of each host.
var readers = map[string]func(string) reading{Claude: readClaude, Codex: readCodex}

// readNow reads a record and notes the size it had before the reading.
func readNow(host, path string) reading {
	info, err := os.Stat(path)
	r := readers[host](path)
	if err == nil {
		r.size = info.Size()
	}
	return r
}

// Record reads the records a notice names and stores their counts under
// dir: the record of the role that ended, when the notice names one, and
// the record of the session, which every notice names. It also reads again
// every record stored for the session that has grown since: the host may
// write a record after its notice. Giving the same notice again writes the
// same files.
func Record(dir, host string, notice Notice) error {
	if readers[host] == nil {
		return fmt.Errorf("unknown host %q; the hosts are %s and %s", host, Claude, Codex)
	}
	session := notice.Session
	if session == "" {
		session = stem(notice.Transcript)
	}
	if session == "" || session == "." {
		return errors.New("the notice names no session")
	}
	var starts []Start
	// Claude Code ends its own agents, such as a prompt suggestion, with an
	// empty agent_type. They are no start.
	internal := host == Claude && notice.AgentID != "" && notice.AgentType == ""
	switch {
	case internal:
	case notice.AgentTranscript != "":
		agent := notice.AgentID
		if agent == "" {
			agent = stem(notice.AgentTranscript)
		}
		starts = append(starts, start(host, session, "agent-"+agent, notice.AgentTranscript, noticeRole(notice.AgentType), readNow(host, notice.AgentTranscript)))
	case notice.AgentID != "":
		// The host says that a role ended and names no record of it: the
		// start is kept, with unknown tokens, so that no sum leaves it out
		// in silence.
		unread := reading{why: "the notice names no record of the role"}
		starts = append(starts, start(host, session, "agent-"+notice.AgentID, "", noticeRole(notice.AgentType), unread))
	}
	if notice.Transcript != "" {
		starts = append(starts, start(host, session, SessionAgent, notice.Transcript, "", readNow(host, notice.Transcript)))
	}
	if len(starts) == 0 && internal {
		return nil
	} else if len(starts) == 0 {
		return errors.New("the notice names no record")
	}
	prior, err := again(dir, session, starts)
	if err != nil {
		return err
	}
	for _, s := range append(prior, starts...) {
		if err := write(dir, s); err != nil {
			return err
		}
	}
	return nil
}

// write stores one start. The file appears whole or not at all.
func write(dir string, s Start) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	folder := filepath.Join(dir, fileName(s.Session))
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(folder, ".tmp-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(append(data, '\n'))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(folder, fileName(s.Agent)+".json"))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// again reads once more the record of each start stored for the session
// that named does not hold, when the record still exists and its size has
// changed. A start whose new reading is unknown keeps its counts.
func again(dir, session string, named []Start) ([]Start, error) {
	files, err := sessionFiles(filepath.Join(dir, fileName(session)))
	if err != nil {
		return nil, err
	}
	var out []Start
	for _, file := range files {
		s, ok := load(file)
		if !ok || s.Session != session || readers[s.Host] == nil || slices.ContainsFunc(named, func(n Start) bool { return n.Agent == s.Agent }) {
			continue
		}
		if info, err := os.Stat(s.Record); s.Record == "" || err != nil || info.Size() == s.Size {
			continue
		}
		if r := readNow(s.Host, s.Record); r.why == "" {
			out = append(out, start(s.Host, s.Session, s.Agent, s.Record, s.Role, r))
		}
	}
	return out, nil
}

// load reads one stored start; ok is false when it cannot be read.
func load(file string) (s Start, ok bool) {
	data, err := os.ReadFile(file)
	ok = err == nil && json.Unmarshal(data, &s) == nil && s.Version == 1 && s.Session != "" && s.Agent != ""
	return s, ok
}

// Load reads every start stored under dir, in the order of their paths. A
// file that cannot be read is named in damaged.
func Load(dir string) (starts []Start, damaged []string, err error) {
	folders, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for _, folder := range folders {
		if !folder.IsDir() {
			continue
		}
		files, err := sessionFiles(filepath.Join(dir, folder.Name()))
		if err != nil {
			return nil, nil, err
		}
		for _, file := range files {
			if s, ok := load(file); ok {
				starts = append(starts, s)
			} else {
				damaged = append(damaged, file)
			}
		}
	}
	return starts, damaged, nil
}

func sessionFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files, nil
}
