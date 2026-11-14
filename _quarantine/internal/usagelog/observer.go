package usagelog

// Observer sees a Claude transcript import line by line, so a caller can
// build its own view of the session beside the report; the experimental
// context composition (package usagelog/composition) is one. It never changes
// the report.
type Observer interface {
	// Session is called once, before any agent, with the session id.
	Session(id string)
	// Agent is called once per transcript file, main first, before that
	// file's lines, and returns the observer of its lines.
	Agent(agent Agent, main bool) LineObserver
}

// LineObserver receives one transcript file's lines by type, in file order.
type LineObserver interface {
	// Assistant gets every assistant line. For a line with valid usage of
	// this session, model and tokens are its request's and recorded reports
	// whether the import counted it (false for a duplicate); otherwise model
	// is empty, tokens are zero and recorded is false. key is the line's
	// de-duplication key, and history reports another session's line copied
	// into a resumed one.
	Assistant(line []byte, key, model string, tokens Tokens, recorded, history bool)
	User(line []byte)
	Attachment(line []byte)
	System(line []byte)
}
