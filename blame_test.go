package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// ---- test helpers ----

type fakeSource struct {
	revs     []Revision
	values   map[int]map[string]any
	computed map[int]map[string]any
	err      error
}

func (f *fakeSource) History(string) ([]Revision, error) { return f.revs, f.err }
func (f *fakeSource) Values(_ string, rev int, computed bool) (map[string]any, error) {
	if f.err != nil {
		return nil, f.err
	}
	if computed {
		return f.computed[rev], nil
	}
	return f.values[rev], nil
}

// parse decodes JSON the same way the real code does (numbers as json.Number).
func parse(t *testing.T, s string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	return m
}

func rev(n int, ts, status, chart string) Revision {
	return Revision{Number: n, Updated: ts, Status: status, Chart: chart}
}

func newFixture(t *testing.T) *fakeSource {
	t.Helper()
	return &fakeSource{
		revs: []Revision{
			rev(1, "2024-03-01T10:00:00+01:00", "superseded", "web-1.0.0"),
			rev(2, "2024-03-02T10:00:00+01:00", "superseded", "web-1.0.0"),
			rev(3, "2024-03-05T09:12:41+01:00", "superseded", "web-1.0.0"),
			rev(4, "2024-04-01T08:00:00+02:00", "deployed", "web-1.1.0"),
		},
		values: map[int]map[string]any{
			1: parse(t, `{"replicas":1,"image":{"repository":"nginx","tag":"1.25"},"db":{"password":"hunter2"},"annotations":{"example.com/team":"a"}}`),
			2: parse(t, `{"replicas":1,"image":{"repository":"nginx","tag":"1.25"},"db":{"password":"hunter3"},"annotations":{"example.com/team":"a"}}`),
			3: parse(t, `{"replicas":3,"image":{"repository":"nginx","tag":"1.26"},"db":{"password":"hunter3"},"annotations":{"example.com/team":"b"}}`),
			4: parse(t, `{"replicas":5,"image":{"repository":"nginx","tag":"1.26"},"ingress":{"enabled":true},"annotations":{"example.com/team":"b"}}`),
		},
		computed: map[int]map[string]any{
			1: parse(t, `{"replicas":1,"resources":{"limits":{"cpu":"100m"}}}`),
			2: parse(t, `{"replicas":1,"resources":{"limits":{"cpu":"100m"}}}`),
			3: parse(t, `{"replicas":3,"resources":{"limits":{"cpu":"100m"}}}`),
			4: parse(t, `{"replicas":5,"resources":{"limits":{"cpu":"250m"}}}`),
		},
	}
}

func cli(t *testing.T, src Source, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	t.Setenv("HELM_NAMESPACE", "")
	t.Setenv("HELM_KUBECONTEXT", "")
	var out, errb bytes.Buffer
	code = run(args, &out, &errb, src)
	return out.String(), errb.String(), code
}

func timelineOf(snaps ...map[string]any) map[string][]Event {
	revs := make([]Revision, len(snaps))
	flat := make([]map[string]any, len(snaps))
	for i, s := range snaps {
		revs[i] = Revision{Number: i + 1, Status: "superseded"}
		flat[i] = flatten(s)
	}
	return buildTimeline(revs, flat)
}

func kinds(tl map[string][]Event, key string) string {
	var parts []string
	for _, e := range tl[key] {
		parts = append(parts, fmt.Sprintf("%d:%s", e.Revision.Number, e.Kind))
	}
	return strings.Join(parts, ",")
}

// ---- flatten ----

func TestFlatten(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want map[string]any
	}{
		{"nested", map[string]any{"a": map[string]any{"b": 1, "c": map[string]any{"d": 2}}, "e": 3},
			map[string]any{"a.b": 1, "a.c.d": 2, "e": 3}},
		{"lists are leaves", map[string]any{"a": []any{1, 2}}, map[string]any{"a": []any{1, 2}}},
		{"empty map is a leaf", map[string]any{"a": map[string]any{}}, map[string]any{"a": map[string]any{}}},
		{"dots escaped", map[string]any{"a": map[string]any{"x.y/z": 1}}, map[string]any{`a.x\.y/z`: 1}},
		{"empty input", map[string]any{}, map[string]any{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := flatten(c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %#v, want %#v", got, c.want)
			}
		})
	}
}

// ---- timeline ----

func TestTimelineKinds(t *testing.T) {
	tl := timelineOf(
		map[string]any{"a": 1, "b": 1},
		map[string]any{"a": 2, "b": 1, "c": 1},
		map[string]any{"a": 2, "c": 1},
	)
	for key, want := range map[string]string{
		"a": "1:initial,2:changed",
		"b": "1:initial,3:removed",
		"c": "2:added",
	} {
		if got := kinds(tl, key); got != want {
			t.Errorf("%s: got %q, want %q", key, got, want)
		}
	}
}

func TestUnchangedKeyHasOneEvent(t *testing.T) {
	tl := timelineOf(map[string]any{"a": 1}, map[string]any{"a": 1}, map[string]any{"a": 1})
	if got := kinds(tl, "a"); got != "1:initial" {
		t.Errorf("got %q", got)
	}
}

func TestValueReturningToPrevious(t *testing.T) {
	tl := timelineOf(map[string]any{"a": 1}, map[string]any{"a": 2}, map[string]any{"a": 1})
	if got := kinds(tl, "a"); got != "1:initial,2:changed,3:changed" {
		t.Errorf("got %q", got)
	}
}

func TestIntBoolFloatAreDistinct(t *testing.T) {
	tl := timelineOf(
		map[string]any{"a": json.Number("1")},
		map[string]any{"a": true},
		map[string]any{"a": json.Number("1.0")},
	)
	if n := len(tl["a"]); n != 3 {
		t.Errorf("got %d events, want 3", n)
	}
}

func TestNullIsDifferentFromUnset(t *testing.T) {
	tl := timelineOf(map[string]any{"a": nil}, map[string]any{})
	if got := kinds(tl, "a"); got != "1:initial,2:removed" {
		t.Errorf("got %q", got)
	}
}

func TestKeyMatches(t *testing.T) {
	if !keyMatches("image.tag", "image") || !keyMatches("image", "image") {
		t.Error("expected match")
	}
	if keyMatches("imagePullPolicy", "image") {
		t.Error("prefix must end on a dot boundary")
	}
}

func TestIsSecret(t *testing.T) {
	for _, k := range []string{"db.password", "apiKey", "api_key", "auth.Token", "tls.privateKey"} {
		if !isSecret(k) {
			t.Errorf("%s should be secret", k)
		}
	}
	if isSecret("replicas") {
		t.Error("replicas is not secret")
	}
}

// ---- helm argument building ----

func TestBuildArgsForwardsGlobalFlags(t *testing.T) {
	h := Helm{Namespace: "prod", KubeContext: "kind", Kubeconfig: "/k"}
	got := strings.Join(h.buildArgs([]string{"history", "x"}), " ")
	want := "history x --namespace prod --kube-context kind --kubeconfig /k"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := strings.Join(Helm{}.buildArgs([]string{"history", "x"}), " "); got != "history x" {
		t.Errorf("no flags expected, got %q", got)
	}
}

func TestMissingHelmBinary(t *testing.T) {
	_, err := Helm{Binary: "/nonexistent/helm"}.History("x")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("got %v", err)
	}
}

// ---- CLI ----

func TestCLISingleKey(t *testing.T) {
	out, errs, code := cli(t, newFixture(t), "myapp", "replicas")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, want := range []string{"replicas", "1 → 3", "3 → 5", "web-1.1.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCLIPrefixQuery(t *testing.T) {
	out, _, code := cli(t, newFixture(t), "myapp", "image")
	if code != 0 || !strings.Contains(out, "image.tag") || !strings.Contains(out, `"1.25" → "1.26"`) {
		t.Errorf("code %d:\n%s", code, out)
	}
}

func TestCLINoKeyListsChangedAndAddedKeys(t *testing.T) {
	out, _, _ := cli(t, newFixture(t), "myapp")
	if !strings.Contains(out, "replicas") {
		t.Error("replicas changed, should be listed")
	}
	if !strings.Contains(out, "ingress.enabled") {
		t.Error("ingress.enabled first appears in rev 4, should be listed")
	}
	if strings.Contains(out, "image.repository") {
		t.Error("image.repository never changed, should be hidden")
	}
}

func TestCLIIncludeUnchanged(t *testing.T) {
	out, _, _ := cli(t, newFixture(t), "myapp", "--include-unchanged")
	if !strings.Contains(out, "image.repository") {
		t.Error("expected unchanged key")
	}
}

func TestCLISecretsMaskedByDefault(t *testing.T) {
	out, _, _ := cli(t, newFixture(t), "myapp", "db.password")
	if strings.Contains(out, "hunter") || !strings.Contains(out, "<redacted>") {
		t.Errorf("secret leaked or not masked:\n%s", out)
	}
}

func TestCLIShowSecrets(t *testing.T) {
	out, _, _ := cli(t, newFixture(t), "myapp", "db.password", "--show-secrets")
	if !strings.Contains(out, "hunter2") || !strings.Contains(out, "(unset)") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestCLIAddedKey(t *testing.T) {
	out, _, _ := cli(t, newFixture(t), "myapp", "ingress.enabled")
	if !strings.Contains(out, "(unset) → true") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestCLIEscapedDotKey(t *testing.T) {
	out, errs, code := cli(t, newFixture(t), "myapp", `annotations.example\.com/team`)
	if code != 0 || !strings.Contains(out, `"a" → "b"`) {
		t.Errorf("code %d, stderr %q:\n%s", code, errs, out)
	}
}

func TestCLIComputed(t *testing.T) {
	out, _, code := cli(t, newFixture(t), "myapp", "resources", "--computed")
	if code != 0 || !strings.Contains(out, `"100m" → "250m"`) {
		t.Errorf("code %d:\n%s", code, out)
	}
	if _, _, code := cli(t, newFixture(t), "myapp", "resources"); code != 1 {
		t.Errorf("without --computed the default is invisible; want exit 1, got %d", code)
	}
}

func TestCLIJSON(t *testing.T) {
	out, errs, code := cli(t, newFixture(t), "myapp", "replicas", "-o", "json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var data []struct {
		Key    string
		Events []struct {
			Revision int
			Kind     string
			New      any
		}
	}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if len(data) != 1 || data[0].Key != "replicas" || len(data[0].Events) != 3 {
		t.Fatalf("unexpected: %+v", data)
	}
	var got []string
	for _, e := range data[0].Events {
		got = append(got, fmt.Sprintf("%d:%s", e.Revision, e.Kind))
	}
	if strings.Join(got, ",") != "1:initial,3:changed,4:changed" {
		t.Errorf("got %v", got)
	}
	if data[0].Events[2].New != float64(5) {
		t.Errorf("last value: %v", data[0].Events[2].New)
	}
}

func TestCLIUnknownKeySuggests(t *testing.T) {
	_, errs, code := cli(t, newFixture(t), "myapp", "replica")
	if code != 1 || !strings.Contains(errs, "not found") || !strings.Contains(errs, "did you mean: replicas") {
		t.Errorf("code %d, stderr %q", code, errs)
	}
}

func TestCLIFlagsBeforePositionals(t *testing.T) {
	if _, errs, code := cli(t, newFixture(t), "-n", "prod", "myapp", "replicas"); code != 0 {
		t.Errorf("exit %d: %s", code, errs)
	}
}

func TestCLIHelmErrorReported(t *testing.T) {
	src := &fakeSource{err: errors.New("Error: release: not found")}
	_, errs, code := cli(t, src, "nope")
	if code != 1 || !strings.Contains(errs, "release: not found") {
		t.Errorf("code %d, stderr %q", code, errs)
	}
}

func TestCLIBadOutputFormat(t *testing.T) {
	if _, _, code := cli(t, newFixture(t), "myapp", "-o", "yaml"); code != 2 {
		t.Errorf("want exit 2, got %d", code)
	}
}

func TestCLIMissingRelease(t *testing.T) {
	if _, _, code := cli(t, newFixture(t)); code != 2 {
		t.Errorf("want exit 2, got %d", code)
	}
}
