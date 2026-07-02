package output

import (
	"bytes"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/a13x22/kube-copy/pkg/conflict"
	"github.com/a13x22/kube-copy/pkg/copier"
)

func TestFormatLocation_Namespaced(t *testing.T) {
	got := FormatLocation("staging", "myapp", true)
	if got != "staging/myapp" {
		t.Errorf("got %q, want staging/myapp", got)
	}
}

func TestFormatLocation_ClusterScoped(t *testing.T) {
	got := FormatLocation("", "fast", false)
	if got != "fast (cluster)" {
		t.Errorf("got %q, want fast (cluster)", got)
	}
}

func TestPrintYAML_MultiDocSeparator(t *testing.T) {
	a := map[string]interface{}{"kind": "ConfigMap", "metadata": map[string]interface{}{"name": "a"}}
	b := map[string]interface{}{"kind": "Secret", "metadata": map[string]interface{}{"name": "b"}}
	results := []copier.CopyResult{
		{Action: "create", Sanitized: &unstructured.Unstructured{Object: a}},
		{Action: "create", Sanitized: &unstructured.Unstructured{Object: b}},
	}

	var buf bytes.Buffer
	if err := printYAML(results, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "kind: ConfigMap") || !strings.Contains(out, "kind: Secret") {
		t.Fatalf("expected both kinds in output:\n%s", out)
	}
	if strings.Count(out, "\n---\n") != 1 {
		t.Fatalf("expected one --- separator, got:\n%s", out)
	}
}

func TestPrintNothingToDo_ReferenceHint(t *testing.T) {
	results := []copier.CopyResult{{
		Action: "skip",
		Conflicts: []conflict.Conflict{{
			Type: conflict.TypeReference,
		}},
	}}
	var buf bytes.Buffer
	PrintNothingToDo(&buf, results)
	if !strings.Contains(buf.String(), "-r") {
		t.Errorf("expected recursive hint, got: %s", buf.String())
	}
}
