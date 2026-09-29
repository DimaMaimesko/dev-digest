package httpapi

import (
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// fields reads the fields of a JSON object request body one at a time and
// collects what is wrong with them, so a handler can check a whole body and
// then answer with every issue at once. Keys nobody asks for are ignored, as
// the TS server's Zod schemas ignore them.
//
// A field that is absent reads as nil. JSON null is an issue, as in Zod,
// except where a method says otherwise.
type fields struct {
	body   map[string]json.RawMessage
	issues []issue
}

func (f *fields) bad(message string, path ...string) {
	f.issues = append(f.issues, issue{Path: path, Message: message})
}

// present reports whether key is in the body, and records an issue when it
// is required and absent.
func (f *fields) present(key string, required bool) bool {
	_, ok := f.body[key]
	if !ok && required {
		f.bad("Required", key)
	}
	return ok
}

// str reads a string. check, when not nil, returns what is wrong with the
// value, or "" when nothing is.
func (f *fields) str(key string, required bool, check func(string) string) *string {
	if !f.present(key, required) {
		return nil
	}
	var s string
	if isNull(f.body[key]) || json.Unmarshal(f.body[key], &s) != nil {
		f.bad("Expected a string", key)
		return nil
	}
	if check != nil {
		if msg := check(s); msg != "" {
			f.bad(msg, key)
			return nil
		}
	}
	return &s
}

// notEmpty is a str check.
func notEmpty(s string) string {
	if s == "" {
		return "Expected a non-empty string"
	}
	return ""
}

// among returns a str check that allows only the given values.
func among(allowed ...string) func(string) string {
	return func(s string) string {
		if !slices.Contains(allowed, s) {
			return "Expected one of: " + strings.Join(allowed, ", ")
		}
		return ""
	}
}

// boolean reads true or false.
func (f *fields) boolean(key string) *bool {
	if !f.present(key, false) {
		return nil
	}
	var b bool
	if isNull(f.body[key]) || json.Unmarshal(f.body[key], &b) != nil {
		f.bad("Expected true or false", key)
		return nil
	}
	return &b
}

// int32 reads a whole number.
func (f *fields) int32(key string) *int32 {
	if !f.present(key, false) {
		return nil
	}
	var n float64
	if isNull(f.body[key]) || json.Unmarshal(f.body[key], &n) != nil ||
		n != math.Trunc(n) || n < math.MinInt32 || n > math.MaxInt32 {
		f.bad("Expected a whole number", key)
		return nil
	}
	v := int32(n)
	return &v
}

// maxSafeInt is the largest whole number a JavaScript number holds exactly,
// the limit of Zod's int().
const maxSafeInt = 1<<53 - 1

// whole reads a whole number that JavaScript holds exactly, such as a GitHub
// ID.
func (f *fields) whole(key string, required bool) *int64 {
	if !f.present(key, required) {
		return nil
	}
	var n float64
	if isNull(f.body[key]) || json.Unmarshal(f.body[key], &n) != nil ||
		n != math.Trunc(n) || math.Abs(n) > maxSafeInt {
		f.bad("Expected a whole number", key)
		return nil
	}
	v := int64(n)
	return &v
}

// anyJSON reads a value of any JSON type, null included, unchanged.
func (f *fields) anyJSON(key string) json.RawMessage {
	return f.body[key]
}

// id reads a UUID in its standard form.
func (f *fields) id(key string) *uuid.UUID {
	s := f.str(key, false, nil)
	if s == nil {
		return nil
	}
	id, err := uuid.Parse(*s)
	if err != nil || len(*s) != 36 {
		f.bad("Invalid uuid", key)
		return nil
	}
	return &id
}

// ids reads an array of UUIDs; ok is false when the key is absent or
// invalid.
func (f *fields) ids(key string) (ids []uuid.UUID, ok bool) {
	if !f.present(key, false) {
		return nil, false
	}
	var raw []string
	if isNull(f.body[key]) || json.Unmarshal(f.body[key], &raw) != nil {
		f.bad("Expected an array of uuids", key)
		return nil, false
	}
	ids = make([]uuid.UUID, 0, len(raw))
	for i, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil || len(s) != 36 {
			f.bad("Invalid uuid", key, strconv.Itoa(i))
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}
