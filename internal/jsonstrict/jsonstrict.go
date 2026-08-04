// Package jsonstrict adds the strictness encoding/json does not provide.
//
// It lives in internal/ because both the root package and api/ need it and
// neither imports the other. Nothing here is part of the public API.
package jsonstrict

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrDuplicateKey is returned when a request body names the same object
// member twice.
var ErrDuplicateKey = errors.New("request body contains a duplicate JSON key")

// RejectDuplicateKeys reports an error if any object in the document names
// the same member twice.
//
// encoding/json silently takes the last occurrence: {"role":"user","role":"admin"}
// unmarshals to "admin" with no error. That is a smuggling primitive whenever
// anything else in the request path -- a proxy, a WAF, a gateway, an audit log
// -- parses the same body and resolves the conflict differently. Two components
// disagreeing about what a request said is the whole bug class.
//
// encoding/json/v2 rejects duplicates, but only through its own API. Go 1.27
// makes v2 the implementation behind encoding/json while deliberately keeping
// v1's semantics, so the permissive behaviour is not going away on its own and
// waiting for it would have been waiting for nothing.
// RejectDuplicateKeys is the exported entry point.
func RejectDuplicateKeys(body []byte) error {
	dec := json.NewDecoder(strings.NewReader(string(body)))

	// One frame per open object or array. Objects carry the member names seen
	// so far and whether the next token is a name or a value -- a string token
	// is only a key in the first position, otherwise {"role":"role"} would
	// report itself as a duplicate.
	type frame struct {
		isObject  bool
		expectKey bool
		keys      map[string]struct{}
	}
	var stack []frame

	// valueConsumed advances the innermost object past a value, so the next
	// string it sees is read as a member name again.
	valueConsumed := func() {
		if n := len(stack); n > 0 && stack[n-1].isObject {
			stack[n-1].expectKey = true
		}
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			// Malformed input is the decoder's business to report, with its own
			// message and position. Stop scanning and let Decode produce it.
			return nil
		}

		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{':
				stack = append(stack, frame{isObject: true, expectKey: true, keys: map[string]struct{}{}})
			case '[':
				stack = append(stack, frame{})
			case '}', ']':
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				// The object or array that just closed was itself a value.
				valueConsumed()
			}
			continue
		}

		if n := len(stack); n > 0 && stack[n-1].isObject && stack[n-1].expectKey {
			name, ok := tok.(string)
			if !ok {
				// Not reachable for valid JSON; a non-string member name would
				// have failed at dec.Token above.
				continue
			}
			if _, seen := stack[n-1].keys[name]; seen {
				return fmt.Errorf("%w: %q", ErrDuplicateKey, name)
			}
			stack[n-1].keys[name] = struct{}{}
			stack[n-1].expectKey = false
			continue
		}

		// A scalar value, either an object member's value or an array element.
		valueConsumed()
	}
}
