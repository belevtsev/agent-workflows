// Evaluates a grader's input_match the way the eval runner does: a
// JavaScript RegExp built from the pattern, without flags, tested against the
// tool input as JSON.stringify encodes it.
//
// Usage: node match.js <cases.json>
// cases.json: {"pattern": "...", "cases": [{"input": "<JSON object text>", "raw": false}]}
// A case's input is parsed and re-encoded with JSON.stringify unless raw is
// true, in which case the text is matched as given. Prints a JSON array of
// {"text": <matched text>, "matched": <bool>}, one per case, in order.
'use strict';
const fs = require('fs');

const { pattern, cases } = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
const re = new RegExp(pattern);
const out = cases.map((c) => {
  const text = c.raw ? c.input : JSON.stringify(JSON.parse(c.input));
  return { text, matched: re.test(text) };
});
process.stdout.write(JSON.stringify(out));
