// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// jnode is a JSON value that keeps the order of object keys. encoding/json maps lose it,
// and the order of a definition's properties decides the order of the generated fields
// (and therefore the key order of the rendered YAML).
type jnode struct {
	kind jkind
	b    bool
	n    json.Number
	s    string
	arr  []*jnode
	keys []string
	vals map[string]*jnode
}

type jkind int

const (
	jNull jkind = iota
	jBool
	jNum
	jStr
	jArr
	jObj
)

func parseOrdered(data []byte) (*jnode, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	n, err := readValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after the JSON document")
	}
	return n, nil
}

func readValue(dec *json.Decoder) (*jnode, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case nil:
		return &jnode{kind: jNull}, nil
	case bool:
		return &jnode{kind: jBool, b: t}, nil
	case json.Number:
		return &jnode{kind: jNum, n: t}, nil
	case string:
		return &jnode{kind: jStr, s: t}, nil
	case json.Delim:
		switch t {
		case '[':
			n := &jnode{kind: jArr}
			for dec.More() {
				v, err := readValue(dec)
				if err != nil {
					return nil, err
				}
				n.arr = append(n.arr, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return n, nil
		case '{':
			n := &jnode{kind: jObj, vals: map[string]*jnode{}}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, ok := kt.(string)
				if !ok {
					return nil, fmt.Errorf("object key is not a string: %v", kt)
				}
				v, err := readValue(dec)
				if err != nil {
					return nil, err
				}
				if _, dup := n.vals[k]; !dup {
					n.keys = append(n.keys, k)
				}
				n.vals[k] = v
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return n, nil
		}
	}
	return nil, fmt.Errorf("unexpected JSON token %v", tok)
}

// get returns the member k of an object, or nil.
func (j *jnode) get(k string) *jnode {
	if j == nil || j.kind != jObj {
		return nil
	}
	return j.vals[k]
}

func (j *jnode) str(k string) string {
	if v := j.get(k); v != nil && v.kind == jStr {
		return v.s
	}
	return ""
}

func (j *jnode) list(k string) []*jnode {
	if v := j.get(k); v != nil && v.kind == jArr {
		return v.arr
	}
	return nil
}

func (j *jnode) strings(k string) []string {
	var out []string
	for _, v := range j.list(k) {
		out = append(out, v.scalarString())
	}
	return out
}

// scalarString renders a scalar for documentation (enum values, defaults).
func (j *jnode) scalarString() string {
	switch j.kind {
	case jStr:
		return j.s
	case jNum:
		return j.n.String()
	case jBool:
		return strconv.FormatBool(j.b)
	case jNull:
		return "null"
	}
	return ""
}

// types returns the JSON schema `type` keyword as a list.
func (j *jnode) types() []string {
	v := j.get("type")
	if v == nil {
		return nil
	}
	if v.kind == jStr {
		return []string{v.s}
	}
	var out []string
	for _, e := range v.arr {
		out = append(out, e.s)
	}
	return out
}
