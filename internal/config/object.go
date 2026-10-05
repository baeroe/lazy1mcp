package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Object is a JSON object that keeps its key order and the raw bytes of every value, so a
// load/modify/save round trip leaves everything it does not touch exactly as it was.
type Object struct {
	keys []string
	vals map[string]json.RawMessage
}

// NewObject returns an empty object.
func NewObject() *Object { return &Object{vals: map[string]json.RawMessage{}} }

// UnmarshalJSON parses a JSON object, keeping key order.
func (o *Object) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return errors.New("expected a JSON object")
	}
	o.keys, o.vals = nil, map[string]json.RawMessage{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		k, ok := t.(string)
		if !ok {
			return fmt.Errorf("unexpected token %v", t)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if _, dup := o.vals[k]; !dup {
			o.keys = append(o.keys, k)
		}
		o.vals[k] = raw
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after JSON object")
	}
	return nil
}

// MarshalJSON writes the object in key order (compact; Indent it for display).
func (o *Object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := marshal(k)
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Keys returns the keys in order.
func (o *Object) Keys() []string { return append([]string(nil), o.keys...) }

// Has reports whether k is set.
func (o *Object) Has(k string) bool { _, ok := o.vals[k]; return ok }

// Raw returns the raw value of k.
func (o *Object) Raw(k string) (json.RawMessage, bool) { v, ok := o.vals[k]; return v, ok }

// Decode unmarshals the value of k into v; it reports false if k is missing.
func (o *Object) Decode(k string, v any) (bool, error) {
	raw, ok := o.vals[k]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, v)
}

// Set marshals v and stores it under k (appended if k is new, in place otherwise).
func (o *Object) Set(k string, v any) error {
	b, err := marshal(v)
	if err != nil {
		return err
	}
	o.SetRaw(k, b)
	return nil
}

// SetRaw stores raw JSON under k.
func (o *Object) SetRaw(k string, raw json.RawMessage) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = raw
}

// Delete removes k.
func (o *Object) Delete(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	for i, kk := range o.keys {
		if kk == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// Rename moves the value of old to new, keeping its position.
func (o *Object) Rename(old, new string) {
	v, ok := o.vals[old]
	if !ok || old == new {
		return
	}
	o.Delete(new)
	delete(o.vals, old)
	o.vals[new] = v
	for i, k := range o.keys {
		if k == old {
			o.keys[i] = new
		}
	}
}

// marshal is json.Marshal without HTML escaping and without the trailing newline.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// indent pretty-prints JSON with two spaces.
func indent(b []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
