package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// version is overridden at release time: -ldflags "-X main.version=1.2.3"
var version = "dev"

const usageText = `Show the per-key history of a release's values across revisions.

Usage:
  helm blame RELEASE [KEY] [flags]

KEY is a value key such as "replicas" or "image.tag". A prefix like "image"
shows everything under it. Omit KEY to show every key that ever changed.

Flags:
`

type options struct {
	namespace, kubeContext, kubeconfig string
	output                             string
	computed, includeUnchanged         bool
	showSecrets, showVersion           bool
	release, key                       string
}

// errReported means the flag package already printed the problem.
var errReported = errors.New("already reported")

func parseArgs(args []string, stderr io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("helm blame", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usageText)
		fs.PrintDefaults()
	}

	// Helm exports these for plugins; they are the defaults.
	ns := os.Getenv("HELM_NAMESPACE")
	kctx := os.Getenv("HELM_KUBECONTEXT")

	fs.StringVar(&o.namespace, "n", ns, "namespace of the release")
	fs.StringVar(&o.namespace, "namespace", ns, "namespace of the release")
	fs.StringVar(&o.kubeContext, "kube-context", kctx, "kubeconfig context to use")
	fs.StringVar(&o.kubeconfig, "kubeconfig", "", "path to the kubeconfig file")
	fs.BoolVar(&o.computed, "computed", false, "include chart default values (like 'helm get values --all')")
	fs.BoolVar(&o.includeUnchanged, "include-unchanged", false, "with no KEY, also list keys that never changed")
	fs.BoolVar(&o.showSecrets, "show-secrets", false, "do not mask values of keys that look like secrets")
	fs.StringVar(&o.output, "o", "table", "output format: table or json")
	fs.StringVar(&o.output, "output", "table", "output format: table or json")
	fs.BoolVar(&o.showVersion, "version", false, "print version and exit")

	// Allow flags before, between and after positional arguments.
	var pos []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return o, err
			}
			return o, errReported
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		pos = append(pos, rest[0])
		rest = rest[1:]
	}

	if o.showVersion {
		return o, nil
	}
	if len(pos) < 1 || len(pos) > 2 {
		fs.Usage()
		return o, errors.New("expected RELEASE and optionally KEY")
	}
	o.release = pos[0]
	if len(pos) == 2 {
		o.key = pos[1]
	}
	if o.output != "table" && o.output != "json" {
		return o, fmt.Errorf("invalid --output %q (use table or json)", o.output)
	}
	return o, nil
}

func hasChange(events []Event) bool {
	for _, e := range events {
		if e.Kind != "initial" {
			return true
		}
	}
	return false
}

// run does all the work and returns the exit code. src can be nil (use real
// helm) or a fake in tests.
func run(args []string, stdout, stderr io.Writer, src Source) int {
	o, err := parseArgs(args, stderr)
	if err != nil {
		switch {
		case errors.Is(err, flag.ErrHelp):
			return 0
		case errors.Is(err, errReported):
			return 2
		}
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}
	if o.showVersion {
		fmt.Fprintln(stdout, "helm-blame", version)
		return 0
	}

	if src == nil {
		bin := os.Getenv("HELM_BIN") // Helm tells plugins which helm is running them
		if bin == "" {
			bin = "helm"
		}
		src = Helm{Binary: bin, Namespace: o.namespace, KubeContext: o.kubeContext, Kubeconfig: o.kubeconfig}
	}

	// 1. Read every revision's values.
	revs, err := src.History(o.release)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if len(revs) == 0 {
		fmt.Fprintf(stderr, "error: no revisions found for release %q\n", o.release)
		return 1
	}
	snaps := make([]map[string]any, 0, len(revs))
	for _, r := range revs {
		v, err := src.Values(o.release, r.Number, o.computed)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		snaps = append(snaps, flatten(v))
	}

	// 2. Work out what changed.
	tl := buildTimeline(revs, snaps)

	// 3. Pick which keys to show.
	var keys []string
	if o.key != "" {
		for p := range tl {
			if keyMatches(p, o.key) {
				keys = append(keys, p)
			}
		}
		if len(keys) == 0 {
			var hints []string
			for p := range tl {
				if strings.Contains(strings.ToLower(p), strings.ToLower(o.key)) {
					hints = append(hints, p)
				}
			}
			sort.Strings(hints)
			if len(hints) > 5 {
				hints = hints[:5]
			}
			msg := fmt.Sprintf("error: key %q not found in any revision of %q", o.key, o.release)
			if len(hints) > 0 {
				msg += "\n  did you mean: " + strings.Join(hints, ", ")
			}
			fmt.Fprintln(stderr, msg)
			return 1
		}
	} else {
		for p, evs := range tl {
			if o.includeUnchanged || hasChange(evs) {
				keys = append(keys, p)
			}
		}
		if len(keys) == 0 {
			fmt.Fprintf(stdout, "No value changes found across %d revision(s) of %q. Use --include-unchanged to list every key.\n",
				len(revs), o.release)
			return 0
		}
	}
	sort.Strings(keys)

	// 4. Print.
	if o.output == "json" {
		fmt.Fprintln(stdout, renderJSON(keys, tl, o.showSecrets))
	} else {
		fmt.Fprintln(stdout, renderTable(keys, tl, o.showSecrets))
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, nil))
}
