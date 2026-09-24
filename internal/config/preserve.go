package config

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// A config file can hold members its Go type does not model: clone writes
// repo.structure/isBare, and in local mode hub and hopspace configs share
// one hop.json, each modeling only part of it. Rewriting the file from the
// Go value alone would drop everything else, so writes carry unmodeled
// members over from the file currently on disk.

type member struct {
	key string
	val json.RawMessage
}

// mergeUnmodeled returns cur (the JSON encoding of a value of type t) with
// every object member of prev that t does not model carried over. Modeled
// members are owned by cur: present ones come from cur, absent ones
// (deleted map entries, cleared omitempty fields) stay absent. Recurses
// into struct fields and string-keyed maps so nested members survive too.
func mergeUnmodeled(t reflect.Type, cur, prev json.RawMessage) json.RawMessage {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t.Kind() == reflect.Struct:
		return mergeStruct(t, cur, prev)
	case t.Kind() == reflect.Map && t.Key().Kind() == reflect.String:
		return mergeMap(t.Elem(), cur, prev)
	}
	return cur
}

func mergeStruct(t reflect.Type, cur, prev json.RawMessage) json.RawMessage {
	curObj, ok := decodeObject(cur)
	if !ok {
		return cur
	}
	prevObj, ok := decodeObject(prev)
	if !ok {
		return cur
	}
	fields := jsonFields(t)

	out := make([]member, 0, len(curObj)+len(prevObj))
	for _, m := range curObj {
		if ft, ok := fields[m.key]; ok {
			if pv, ok := lookup(prevObj, m.key); ok {
				m.val = mergeUnmodeled(ft, m.val, pv)
			}
		}
		out = append(out, m)
	}

	var extra []member
	for _, m := range prevObj {
		if !isModeled(fields, m.key) {
			extra = append(extra, m)
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].key < extra[j].key })
	return encodeObject(append(out, extra...))
}

func mergeMap(elem reflect.Type, cur, prev json.RawMessage) json.RawMessage {
	curObj, ok := decodeObject(cur)
	if !ok {
		return cur
	}
	prevObj, ok := decodeObject(prev)
	if !ok {
		return cur
	}
	prevByKey := make(map[string]json.RawMessage, len(prevObj))
	for _, m := range prevObj {
		prevByKey[m.key] = m.val
	}
	for i, m := range curObj {
		if pv, ok := prevByKey[m.key]; ok {
			curObj[i].val = mergeUnmodeled(elem, m.val, pv)
		}
	}
	return encodeObject(curObj)
}

// renameBranchKeys returns prev with each branches member named by a key
// of renames rekeyed to its value, so a renamed entry merges with its own
// previous members. A rename whose target key is already on disk is left
// alone: that entry is the one the new key names.
func renameBranchKeys(prev json.RawMessage, renames map[string]string) json.RawMessage {
	if len(renames) == 0 {
		return prev
	}
	top, ok := decodeObject(prev)
	if !ok {
		return prev
	}
	for i, m := range top {
		if m.key != "branches" {
			continue
		}
		branches, ok := decodeObject(m.val)
		if !ok {
			return prev
		}
		present := make(map[string]bool, len(branches))
		for _, b := range branches {
			present[b.key] = true
		}
		for j, b := range branches {
			if to, ok := renames[b.key]; ok && !present[to] {
				branches[j].key = to
			}
		}
		top[i].val = encodeObject(branches)
	}
	return encodeObject(top)
}

// jsonFields maps the JSON member names encoding/json uses for t's fields
// to their types, flattening embedded structs.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for k, v := range jsonFields(ft) {
					fields[k] = v
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields[name] = f.Type
	}
	return fields
}

// isModeled reports whether encoding/json would decode key into one of
// fields; it matches member names case-insensitively.
func isModeled(fields map[string]reflect.Type, key string) bool {
	if _, ok := fields[key]; ok {
		return true
	}
	for name := range fields {
		if strings.EqualFold(name, key) {
			return true
		}
	}
	return false
}

// lookup finds the prev member encoding/json would decode into the field
// named key: exact match first, then case-insensitive.
func lookup(obj []member, key string) (json.RawMessage, bool) {
	for _, m := range obj {
		if m.key == key {
			return m.val, true
		}
	}
	for _, m := range obj {
		if strings.EqualFold(m.key, key) {
			return m.val, true
		}
	}
	return nil, false
}

// decodeObject splits a JSON object into its members, in document order.
func decodeObject(data json.RawMessage) ([]member, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, false
	}
	var members []member
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, _ := tok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, false
		}
		members = append(members, member{key: key, val: val})
	}
	return members, true
}

func encodeObject(members []member) json.RawMessage {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, _ := json.Marshal(m.key)
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(m.val)
	}
	buf.WriteByte('}')
	return buf.Bytes()
}
