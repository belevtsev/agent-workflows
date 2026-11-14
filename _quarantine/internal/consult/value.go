package consult

import (
	"bytes"
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Consultation evidence is decoded into a small dynamic model instead of
// structs. A record must keep the prepared request verbatim, including its key
// order and whether each number was an integer, so that it re-encodes to the
// same bytes and the same request_sha256 as records already in task storage:
//
//	null -> nil, boolean -> bool, string -> string, array -> []any,
//	object -> *object, integer -> integer, other number -> float64
//
// A string can hold an unpaired surrogate escape, which is retained as its
// generalized UTF-8 sequence because evidence must never be silently rewritten.

// Integers are arbitrary precision, so they stay in canonical decimal text.
type integer string

func number(value int) integer { return integer(strconv.Itoa(value)) }

func (value integer) negative() bool { return strings.HasPrefix(string(value), "-") }

func (value integer) float() float64 {
	result, _ := strconv.ParseFloat(string(value), 64)
	return result
}

// object is a JSON object that remembers member insertion order.
type object struct {
	names  []string
	values map[string]any
}

func newObject(pairs ...any) *object {
	result := &object{values: make(map[string]any, len(pairs)/2)}
	for index := 0; index+1 < len(pairs); index += 2 {
		result.set(pairs[index].(string), pairs[index+1])
	}
	return result
}

func (o *object) set(name string, value any) {
	if _, present := o.values[name]; !present {
		o.names = append(o.names, name)
	}
	o.values[name] = value
}

func (o *object) get(name string) any { return o.values[name] }

func (o *object) has(name string) bool {
	_, present := o.values[name]
	return present
}

func (o *object) size() int { return len(o.names) }

// with returns a shallow copy that adds or replaces one member.
func (o *object) with(name string, value any) *object {
	result := newObject()
	for _, member := range o.names {
		result.set(member, o.values[member])
	}
	result.set(name, value)
	return result
}

// exactly reports whether the object has precisely these member names.
func (o *object) exactly(names ...string) bool {
	if o.size() != len(names) {
		return false
	}
	for _, name := range names {
		if !o.has(name) {
			return false
		}
	}
	return true
}

func (o *object) sameNames(other *object) bool { return o.exactly(other.names...) }

func text(value any) (string, bool) {
	result, ok := value.(string)
	return result, ok
}

func mapping(value any) (*object, bool) {
	result, ok := value.(*object)
	return result, ok && result != nil
}

// blank matches the helper's strip() test, which also treats the ASCII
// separators U+001C through U+001F as whitespace.
func blank(value string) bool {
	return strings.TrimFunc(value, func(r rune) bool { return unicode.IsSpace(r) || r >= 0x1c && r <= 0x1f }) == ""
}

// nextCodePoint decodes one scalar value or one retained unpaired surrogate.
func nextCodePoint(value string) (rune, int) {
	r, size := utf8.DecodeRuneInString(value)
	if r == utf8.RuneError && size == 1 && len(value) >= 3 && value[0] == 0xed && value[1]&0xe0 == 0xa0 {
		return rune(value[0]&0x0f)<<12 | rune(value[1]&0x3f)<<6 | rune(value[2]&0x3f), 3
	}
	return r, size
}

func codePoints(value string) int {
	count := 0
	for len(value) > 0 {
		_, size := nextCodePoint(value)
		value = value[size:]
		count++
	}
	return count
}

// rational returns the exact value of a JSON number or boolean. Infinities have
// no rational value and compare only with themselves.
func rational(value any) (*big.Rat, bool) {
	switch item := value.(type) {
	case bool:
		if item {
			return big.NewRat(1, 1), true
		}
		return new(big.Rat), true
	case integer:
		return new(big.Rat).SetString(string(item))
	case float64:
		if math.IsInf(item, 0) || math.IsNaN(item) {
			return nil, false
		}
		return new(big.Rat).SetFloat64(item), true
	default:
		return nil, false
	}
}

// equal compares decoded values as the helper always has. Object member order is
// insignificant, and numbers compare by value across integer, float and boolean.
func equal(left, right any) bool {
	if a, ok := rational(left); ok {
		b, ok := rational(right)
		return ok && a.Cmp(b) == 0
	}
	switch a := left.(type) {
	case nil:
		return right == nil
	case string:
		b, ok := right.(string)
		return ok && a == b
	case float64:
		b, ok := right.(float64)
		return ok && a == b
	case []any:
		b, ok := right.([]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for index := range a {
			if !equal(a[index], b[index]) {
				return false
			}
		}
		return true
	case *object:
		b, ok := right.(*object)
		if !ok || !a.sameNames(b) {
			return false
		}
		for _, name := range a.names {
			if !equal(a.get(name), b.get(name)) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

const (
	maxJSONDepth     = 1000
	maxIntegerDigits = 4300
)

var errInvalidJSON = errors.New("invalid JSON")

type decoder struct {
	data     []byte
	position int
}

// parseJSON strictly decodes one UTF-8 JSON document. It rejects duplicate
// names and the NaN and Infinity constants, while an out-of-range float becomes
// an infinity that later finite-value checks refuse.
func parseJSON(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, errInvalidJSON
	}
	input := decoder{data: data}
	input.skipSpace()
	value, err := input.value(0)
	if err != nil {
		return nil, err
	}
	input.skipSpace()
	if input.position != len(data) {
		return nil, errInvalidJSON
	}
	return value, nil
}

func (d *decoder) skipSpace() {
	for d.position < len(d.data) {
		switch d.data[d.position] {
		case ' ', '\t', '\n', '\r':
			d.position++
		default:
			return
		}
	}
}

func (d *decoder) literal(word string, value any) (any, error) {
	if !bytes.HasPrefix(d.data[d.position:], []byte(word)) {
		return nil, errInvalidJSON
	}
	d.position += len(word)
	return value, nil
}

func (d *decoder) value(depth int) (any, error) {
	if depth > maxJSONDepth || d.position >= len(d.data) {
		return nil, errInvalidJSON
	}
	switch character := d.data[d.position]; {
	case character == '{':
		return d.object(depth)
	case character == '[':
		return d.array(depth)
	case character == '"':
		return d.text()
	case character == 'n':
		return d.literal("null", nil)
	case character == 't':
		return d.literal("true", true)
	case character == 'f':
		return d.literal("false", false)
	default:
		return d.number()
	}
}

func (d *decoder) object(depth int) (any, error) {
	result := newObject()
	d.position++
	d.skipSpace()
	if d.position < len(d.data) && d.data[d.position] == '}' {
		d.position++
		return result, nil
	}
	for {
		if d.position >= len(d.data) || d.data[d.position] != '"' {
			return nil, errInvalidJSON
		}
		name, err := d.text()
		if err != nil {
			return nil, err
		}
		if result.has(name) {
			return nil, errInvalidJSON
		}
		d.skipSpace()
		if d.position >= len(d.data) || d.data[d.position] != ':' {
			return nil, errInvalidJSON
		}
		d.position++
		d.skipSpace()
		value, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		result.set(name, value)
		more, err := d.separator('}')
		if err != nil || !more {
			return result, err
		}
	}
}

func (d *decoder) array(depth int) (any, error) {
	result := []any{}
	d.position++
	d.skipSpace()
	if d.position < len(d.data) && d.data[d.position] == ']' {
		d.position++
		return result, nil
	}
	for {
		value, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
		more, err := d.separator(']')
		if err != nil || !more {
			return result, err
		}
	}
}

// separator consumes a comma, or the closing delimiter that ends a container.
func (d *decoder) separator(closing byte) (bool, error) {
	d.skipSpace()
	if d.position >= len(d.data) {
		return false, errInvalidJSON
	}
	character := d.data[d.position]
	d.position++
	if character == closing {
		return false, nil
	}
	if character != ',' {
		return false, errInvalidJSON
	}
	d.skipSpace()
	return true, nil
}

func (d *decoder) digits() int {
	start := d.position
	for d.position < len(d.data) && d.data[d.position] >= '0' && d.data[d.position] <= '9' {
		d.position++
	}
	return d.position - start
}

func (d *decoder) number() (any, error) {
	start := d.position
	if d.data[d.position] == '-' {
		d.position++
	}
	digits := d.position
	if d.digits() == 0 || d.data[digits] == '0' && d.position > digits+1 {
		return nil, errInvalidJSON
	}
	whole := d.position
	isFloat := false
	if d.position < len(d.data) && d.data[d.position] == '.' {
		d.position++
		if d.digits() == 0 {
			return nil, errInvalidJSON
		}
		isFloat = true
	}
	if d.position < len(d.data) && (d.data[d.position] == 'e' || d.data[d.position] == 'E') {
		d.position++
		if d.position < len(d.data) && (d.data[d.position] == '+' || d.data[d.position] == '-') {
			d.position++
		}
		if d.digits() == 0 {
			return nil, errInvalidJSON
		}
		isFloat = true
	}
	literal := string(d.data[start:d.position])
	if isFloat {
		// A range error still returns the signed infinity or underflowed value.
		value, _ := strconv.ParseFloat(literal, 64)
		return value, nil
	}
	if whole-digits > maxIntegerDigits {
		return nil, errInvalidJSON
	}
	if literal == "-0" {
		literal = "0"
	}
	return integer(literal), nil
}

func (d *decoder) text() (string, error) {
	d.position++
	var result []byte
	start := d.position
	for d.position < len(d.data) {
		switch character := d.data[d.position]; {
		case character == '"':
			result = append(result, d.data[start:d.position]...)
			d.position++
			return string(result), nil
		case character < 0x20:
			return "", errInvalidJSON
		case character == '\\':
			result = append(result, d.data[start:d.position]...)
			var err error
			if result, err = d.escape(result); err != nil {
				return "", err
			}
			start = d.position
		default:
			d.position++
		}
	}
	return "", errInvalidJSON
}

func (d *decoder) hex(position int) (rune, bool) {
	if position+4 > len(d.data) {
		return 0, false
	}
	value, err := strconv.ParseUint(string(d.data[position:position+4]), 16, 16)
	return rune(value), err == nil
}

func (d *decoder) escape(result []byte) ([]byte, error) {
	if d.position+1 >= len(d.data) {
		return nil, errInvalidJSON
	}
	character := d.data[d.position+1]
	d.position += 2
	switch character {
	case '"', '\\', '/':
		return append(result, character), nil
	case 'b':
		return append(result, '\b'), nil
	case 'f':
		return append(result, '\f'), nil
	case 'n':
		return append(result, '\n'), nil
	case 'r':
		return append(result, '\r'), nil
	case 't':
		return append(result, '\t'), nil
	case 'u':
		first, ok := d.hex(d.position)
		if !ok {
			return nil, errInvalidJSON
		}
		d.position += 4
		if first >= 0xd800 && first <= 0xdbff && bytes.HasPrefix(d.data[d.position:], []byte(`\u`)) {
			second, ok := d.hex(d.position + 2)
			if !ok {
				return nil, errInvalidJSON
			}
			if second >= 0xdc00 && second <= 0xdfff {
				d.position += 6
				return utf8.AppendRune(result, (first-0xd800)<<10+(second-0xdc00)+0x10000), nil
			}
		}
		if first >= 0xd800 && first <= 0xdfff {
			return append(result, 0xe0|byte(first>>12), 0x80|byte(first>>6)&0x3f, 0x80|byte(first)&0x3f), nil //nolint:gosec // G115: first is a lone surrogate; each byte keeps only its UTF-8 bits.
		}
		return utf8.AppendRune(result, first), nil
	default:
		return nil, errInvalidJSON
	}
}

// format selects one of the JSON renderings shared with existing records: key
// order, two-space indentation, and whether non-ASCII text is escaped.
type format struct {
	sorted, indented, unicode bool
}

var (
	wireFormat        = format{}
	canonicalFormat   = format{sorted: true}
	recordFormat      = format{indented: true}
	reportFormat      = format{sorted: true, indented: true, unicode: true}
	offlinePlanFormat = format{sorted: true, indented: true}
)

func (f format) marshal(value any) ([]byte, error) { return f.append(nil, value, 0) }

func (f format) append(out []byte, value any, level int) ([]byte, error) {
	switch item := value.(type) {
	case nil:
		return append(out, "null"...), nil
	case bool:
		return strconv.AppendBool(out, item), nil
	case integer:
		return append(out, item...), nil
	case float64:
		if math.IsInf(item, 0) || math.IsNaN(item) {
			return nil, errors.New("nonfinite JSON number")
		}
		return appendFloat(out, item), nil
	case string:
		return f.appendText(out, item)
	case []any:
		return f.appendArray(out, item, level)
	case *object:
		return f.appendObject(out, item, level)
	default:
		return nil, errors.New("unsupported JSON value")
	}
}

func (f format) newline(out []byte, level int) []byte {
	if !f.indented {
		return out
	}
	out = append(out, '\n')
	for range level {
		out = append(out, "  "...)
	}
	return out
}

func (f format) appendArray(out []byte, values []any, level int) ([]byte, error) {
	if len(values) == 0 {
		return append(out, "[]"...), nil
	}
	out = append(out, '[')
	var err error
	for index, value := range values {
		if index > 0 {
			out = append(out, ',')
		}
		out = f.newline(out, level+1)
		if out, err = f.append(out, value, level+1); err != nil {
			return nil, err
		}
	}
	return append(f.newline(out, level), ']'), nil
}

func (f format) appendObject(out []byte, value *object, level int) ([]byte, error) {
	if value.size() == 0 {
		return append(out, "{}"...), nil
	}
	names := value.names
	if f.sorted {
		// Byte order of generalized UTF-8 is code point order.
		names = sortedNames(value)
	}
	out = append(out, '{')
	var err error
	for index, name := range names {
		if index > 0 {
			out = append(out, ',')
		}
		out = f.newline(out, level+1)
		if out, err = f.appendText(out, name); err != nil {
			return nil, err
		}
		out = append(out, ':')
		if f.indented {
			out = append(out, ' ')
		}
		if out, err = f.append(out, value.get(name), level+1); err != nil {
			return nil, err
		}
	}
	return append(f.newline(out, level), '}'), nil
}

func (f format) appendText(out []byte, value string) ([]byte, error) {
	const hex = "0123456789abcdef"
	unicodeEscape := func(code rune) {
		out = append(out, '\\', 'u', hex[code>>12&15], hex[code>>8&15], hex[code>>4&15], hex[code&15])
	}
	out = append(out, '"')
	for len(value) > 0 {
		r, size := nextCodePoint(value)
		switch {
		case r == '"' || r == '\\':
			out = append(out, '\\', byte(r))
		case r == '\b':
			out = append(out, '\\', 'b')
		case r == '\f':
			out = append(out, '\\', 'f')
		case r == '\n':
			out = append(out, '\\', 'n')
		case r == '\r':
			out = append(out, '\\', 'r')
		case r == '\t':
			out = append(out, '\\', 't')
		case r < 0x20:
			unicodeEscape(r)
		case f.unicode && r >= 0xd800 && r <= 0xdfff:
			return nil, errors.New("unpaired surrogate cannot be written as UTF-8")
		case f.unicode || r >= 0x20 && r < 0x7f:
			out = append(out, value[:size]...)
		case r >= 0x10000:
			r -= 0x10000
			unicodeEscape(0xd800 + r>>10)
			unicodeEscape(0xdc00 + r&0x3ff)
		default:
			unicodeEscape(r)
		}
		value = value[size:]
	}
	return append(out, '"'), nil
}

// appendFloat writes the shortest round-trip form, in fixed notation from 1e-4
// up to 1e16 and exponent notation elsewhere, always marking it as a float.
func appendFloat(out []byte, value float64) []byte {
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	exponent, _ := strconv.Atoi(scientific[strings.IndexByte(scientific, 'e')+1:])
	if value != 0 && (exponent < -4 || exponent >= 16) {
		return append(out, scientific...)
	}
	out = strconv.AppendFloat(out, value, 'f', -1, 64)
	if math.Trunc(value) == value {
		out = append(out, ".0"...)
	}
	return out
}
