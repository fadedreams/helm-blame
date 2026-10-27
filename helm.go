package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// Source is where revisions and values come from. The real one runs helm;
// tests use a fake.
type Source interface {
	History(release string) ([]Revision, error)
	Values(release string, revision int, computed bool) (map[string]any, error)
}

// Helm is the real Source: it shells out to the helm binary.
type Helm struct {
	Binary, Namespace, KubeContext, Kubeconfig string
}

// buildArgs appends the global flags (namespace, context, kubeconfig) to a command.
func (h Helm) buildArgs(args []string) []string {
	out := append([]string{}, args...)
	if h.Namespace != "" {
		out = append(out, "--namespace", h.Namespace)
	}
	if h.KubeContext != "" {
		out = append(out, "--kube-context", h.KubeContext)
	}
	if h.Kubeconfig != "" {
		out = append(out, "--kubeconfig", h.Kubeconfig)
	}
	return out
}

func (h Helm) run(args ...string) ([]byte, error) {
	cmd := exec.Command(h.Binary, h.buildArgs(args)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("cannot run %q: not found in PATH", h.Binary)
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, errors.New(msg) // helm's own message, e.g. "release: not found"
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// History returns the release's revisions, oldest first.
func (h Helm) History(release string) ([]Revision, error) {
	out, err := h.run("history", release, "-o", "json")
	if err != nil {
		return nil, err
	}
	var revs []Revision
	if len(bytes.TrimSpace(out)) > 0 {
		if err := json.Unmarshal(out, &revs); err != nil {
			return nil, fmt.Errorf("unexpected output from helm history: %w", err)
		}
	}
	sort.Slice(revs, func(i, j int) bool { return revs[i].Number < revs[j].Number })
	return revs, nil
}

// Values returns one revision's values. By default these are the values you
// supplied (-f / --set); with computed=true they include the chart defaults.
func (h Helm) Values(release string, revision int, computed bool) (map[string]any, error) {
	args := []string{"get", "values", release, "--revision", strconv.Itoa(revision), "-o", "json"}
	if computed {
		args = append(args, "--all")
	}
	out, err := h.run(args...)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber() // keep 1 and 1.0 as written instead of squashing to float64
	var v any
	if err := dec.Decode(&v); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected output from helm get values: %w", err)
	}
	m, _ := v.(map[string]any) // helm prints "null" when there are no values
	return m, nil
}
