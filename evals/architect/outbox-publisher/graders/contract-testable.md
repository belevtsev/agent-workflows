---
type: llm
focus: { source: file, path: artefacts/interface-contract.md }
---

PASS if every numbered requirement (R1, R2, ...) states inputs, outputs,
errors or observable side effects that a test could check through the
public interface alone, the public types and operations are listed in the
target language's notation, and agreed requirements are separated from
observed current behaviour and open questions.
FAIL if a requirement can only be verified by reading implementation
internals, if requirements are prose paragraphs without numbers, or if the
file mixes design rationale into the requirements.
