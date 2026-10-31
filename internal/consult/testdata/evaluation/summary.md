# Synthetic multilingual evaluation

Status: incomplete.

These labeled synthetic results measure this corpus only. They do not select an adviser,
prove production quality, or resolve real workflow decisions. Consultation decisions remain pending.

| Adviser | Correct | Incorrect | Missing | Accuracy (valid answers) | Coverage | Correct / total |
|---|---:|---:|---:|---:|---:|---:|
| jev | 24 | 0 | 12 | 100.0% | 66.7% | 66.7% |
| subq_snap | 22 | 2 | 12 | 91.7% | 66.7% | 61.1% |
| openai_decisions | 27 | 8 | 1 | 77.1% | 97.2% | 75.0% |

| Adviser | Language | Correct | Incorrect | Missing | Accuracy | Coverage | Correct / total |
|---|---|---:|---:|---:|---:|---:|---:|
| jev | en | 12 | 0 | 0 | 100.0% | 100.0% | 100.0% |
| jev | uk | 12 | 0 | 0 | 100.0% | 100.0% | 100.0% |
| jev | de | 0 | 0 | 12 | unknown | 0.0% | 0.0% |
| subq_snap | en | 10 | 2 | 0 | 83.3% | 100.0% | 83.3% |
| subq_snap | uk | 0 | 0 | 12 | unknown | 0.0% | 0.0% |
| subq_snap | de | 12 | 0 | 0 | 100.0% | 100.0% | 100.0% |
| openai_decisions | en | 11 | 0 | 1 | 100.0% | 91.7% | 91.7% |
| openai_decisions | uk | 12 | 0 | 0 | 100.0% | 100.0% | 100.0% |
| openai_decisions | de | 4 | 8 | 0 | 33.3% | 100.0% | 33.3% |

| Adviser | Consistent / complete triplets | Incomplete triplets | Consistency |
|---|---:|---:|---:|
| jev | 0 / 0 | 12 | unknown |
| subq_snap | 0 / 0 | 12 | unknown |
| openai_decisions | 4 / 11 | 1 | 36.4% |

Case outcomes, requested/returned model identities, whole-batch timing,
provider timing and token usage are retained in report.json.
Consistency is agreement across translations, not correctness. Missing answers are not scored as labels.
Token usage is measured per provider batch; no per-case allocation is inferred.
