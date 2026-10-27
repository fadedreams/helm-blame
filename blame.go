package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

// Revision is one entry from `helm history`.
type Revision struct {
	Number      int    `json:"revision"`
	Updated     string `json:"updated"`
	Status      string `json:"status"`
	Chart       string `json:"chart"`
	Description string `json:"description"`
}

// Event is one change to one key, at one revision.
// Kind is one of: initial | added | changed | removed.
// OldSet/NewSet say whether the key existed before/after, because a key can
// legitimately be set to null, and that is different from "not there".
type Event struct {
	Revision       Revision
	Kind           string
	Old, New       any
	OldSet, NewSet bool
}

// A key name that contains a literal dot (e.g. an annotation) is written "\.".
func escapeKey(k string) string { return strings.ReplaceAll(k, ".", `\.`) }

// flatten turns {"a": {"b": 1}} into {"a.b": 1}.
// Lists and empty maps are kept whole as leaves.
func flatten(v any) map[string]any {
	out := map[string]any{}
	flattenInto(out, "", v)
	return out
}

func flattenInto(out map[string]any, prefix string, v any) {
	if m, ok := v.(map[string]any); ok && len(m) > 0 {
		for k, child := range m {
			path := escapeKey(k)
			if prefix != "" {
				path = prefix + "." + path
			}
			flattenInto(out, path, child)
		}
		return
	}
	if prefix != "" {
		out[prefix] = v
	}
}

// same compares two values by their JSON form, so 1, 1.0 and true differ.
func same(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb)
}

// buildTimeline compares each revision with the one before it and records
// every change per key. revs and snaps are parallel and ordered oldest first.
func buildTimeline(revs []Revision, snaps []map[string]any) map[string][]Event {
	timeline := map[string][]Event{}
	prev := map[string]any{}
	for i, rev := range revs {
		cur := snaps[i]

		keys := map[string]struct{}{} // every key seen in either revision
		for k := range prev {
			keys[k] = struct{}{}
		}
		for k := range cur {
			keys[k] = struct{}{}
		}

		for k := range keys {
			old, oldSet := prev[k]
			nw, newSet := cur[k]

			var kind string
			switch {
			case i == 0:
				kind = "initial"
			case oldSet && newSet && same(old, nw):
				continue // unchanged: no event
			case !oldSet:
				kind = "added"
			case !newSet:
				kind = "removed"
			default:
				kind = "changed"
			}
			timeline[k] = append(timeline[k], Event{
				Revision: rev, Kind: kind,
				Old: old, New: nw, OldSet: oldSet, NewSet: newSet,
			})
		}
		prev = cur
	}
	return timeline
}

// keyMatches: "image" matches "image" and "image.tag", but not "imagePullPolicy".
func keyMatches(path, query string) bool {
	return path == query || strings.HasPrefix(path, query+".")
}

var secretRe = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|private[_-]?key|credential)`)

func isSecret(path string) bool { return secretRe.MatchString(path) }
