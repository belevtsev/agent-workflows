# Numbers and text

Use this when code handles money, float comparisons, IDs crossing a JSON boundary, or user-visible strings. These bugs look correct in review and pass ordinary tests.

## Money and floats

Use a float only for values that are measured, such as physics, statistics, and machine learning. Use exact types for anything counted: money, quantities, and IDs. Keep money as `int64` minor units, and watch currencies with zero or three decimal places. Use `NUMERIC` in the database, never `DOUBLE PRECISION`. Choose the rounding mode deliberately.

| Hazard | Behavior in Go | Handling |
| --- | --- | --- |
| `NaN` | `NaN == NaN` is false, and it spreads through arithmetic | Test with `math.IsNaN`. A `NaN` map key can never be found again |
| Long sums | Adding `0.1` ten million times gives `999999.99984` | Use Kahan summation or integers |
| Subtracting near-equal values | Most correct digits cancel out | Rearrange, or use `math.Log1p`, `math.Expm1`, `math.Hypot` |
| Past 2⁵³ | `float64` can't hold every integer: 2⁵³+1 rounds down | Keep IDs as integers or strings |
| Overflow | Produces `Inf` with no error | Check where inputs are unbounded |

For comparing floats, see the tolerance guidance in [practices](practices.md).

## 64-bit IDs in JSON

JavaScript numbers are doubles, so an `int64` sent as a JSON number can arrive as a different ID. Go hits the same thing: decoding into `any` produces a `float64`, which silently changes `9007199254740993` to `9007199254740992`.

```go
// IDs are strings on the wire, so every consumer gets the exact value.
type Account struct {
	ID int64 `json:"id,string"`
}

// decodeExact keeps numbers exact when decoding into any.
func decodeExact(r io.Reader, out *map[string]any) error {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	return dec.Decode(out)
}
```

## Length has three meanings

For the family emoji, `len(s)` is 25 bytes and `utf8.RuneCountInString(s)` is 7 runes, but a user sees one character. Use bytes for storage limits and graphemes for anything a user sees or counts, and state which one a limit means. Grapheme segmentation needs a library such as `rivo/uniseg`.

Don't slice a string by index for display. `s[:n]` can cut a character in half and produce invalid UTF-8, and even a `[]rune` slice can split an emoji or separate an accent from its letter. To cut to a byte limit, truncate on a character boundary at or below the limit.

## Normalization

The same visible text has several encodings. `é` can be one code point or `e` plus a combining accent. They render identically and compare unequal, and Go's `==` compares bytes. So a uniqueness check can pass for what a person sees as a duplicate.

Normalize to NFC once at the input boundary with `golang.org/x/text/unicode/norm`, and store the normalized form. macOS can produce decomposed file names, so one name can look identical across systems and still differ. NFKC folds more aggressively, so use it for matching identifiers and not for stored display text.

## Case-insensitive comparison

`strings.EqualFold` uses simple case folding, so it reports `straße` and `STRASSE` as different. Use `golang.org/x/text/cases` with `cases.Fold()` where full folding matters, and normalize first. Don't compare with `strings.ToLower` for protocol tokens or keys. Lowercase an email's domain, and leave the local part alone.

## Identifiers from users

Text that identifies a user or resource can be spoofed: Cyrillic letters can look identical to Latin ones. Apply NFKC, restrict to an allowed script or reject mixed scripts, and reject zero-width characters and bidirectional controls. Those controls can make displayed text differ from its real content.

_Adapted from `floating-point` and `unicode-pitfalls` in `coding-skills` by auralshin, and rewritten for Go after running each behavior described here._
