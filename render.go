package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
)

// toJSON renders a value compactly, without escaping < > & (so "<redacted>"
// stays readable).
func toJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimRight(buf.String(), "\n")
}

func fmtValue(v any, set bool, path string, showSecrets bool) string {
	if !set {
		return "(unset)"
	}
	if !showSecrets && isSecret(path) {
		return "<redacted>"
	}
	s := toJSON(v)
	if r := []rune(s); len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return s
}

func fmtChange(e Event, path string, showSecrets bool) string {
	nw := fmtValue(e.New, e.NewSet, path, showSecrets)
	if e.Kind == "initial" {
		return nw + "  (first revision seen)"
	}
	return fmtValue(e.Old, e.OldSet, path, showSecrets) + " → " + nw
}

// "2024-03-05T09:12:41.123+01:00" -> "2024-03-05 09:12:41"
func shortTime(s string) string {
	if len(s) > 19 {
		s = s[:19]
	}
	return strings.Replace(s, "T", " ", 1)
}

func renderTable(keys []string, tl map[string][]Event, showSecrets bool) string {
	var blocks []string
	for _, path := range keys {
		var buf bytes.Buffer
		tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  REV\tUPDATED\tSTATUS\tCHART\tCHANGE")
		for _, e := range tl[path] {
			r := e.Revision
			fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\t%s\n",
				r.Number, shortTime(r.Updated), r.Status, r.Chart,
				fmtChange(e, path, showSecrets))
		}
		tw.Flush()
		blocks = append(blocks, path+"\n"+strings.TrimRight(buf.String(), "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

type jsonEvent struct {
	Revision int    `json:"revision"`
	Updated  string `json:"updated"`
	Status   string `json:"status"`
	Chart    string `json:"chart"`
	Kind     string `json:"kind"`
	Old      any    `json:"old"`
	New      any    `json:"new"`
}

type jsonKey struct {
	Key    string      `json:"key"`
	Events []jsonEvent `json:"events"`
}

func jsonVal(v any, set bool, path string, showSecrets bool) any {
	if !set {
		return nil
	}
	if !showSecrets && isSecret(path) {
		return "<redacted>"
	}
	return v
}

func renderJSON(keys []string, tl map[string][]Event, showSecrets bool) string {
	out := make([]jsonKey, 0, len(keys))
	for _, path := range keys {
		jk := jsonKey{Key: path, Events: []jsonEvent{}}
		for _, e := range tl[path] {
			jk.Events = append(jk.Events, jsonEvent{
				Revision: e.Revision.Number,
				Updated:  e.Revision.Updated,
				Status:   e.Revision.Status,
				Chart:    e.Revision.Chart,
				Kind:     e.Kind,
				Old:      jsonVal(e.Old, e.OldSet, path, showSecrets),
				New:      jsonVal(e.New, e.NewSet, path, showSecrets),
			})
		}
		out = append(out, jk)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	return strings.TrimRight(buf.String(), "\n")
}
