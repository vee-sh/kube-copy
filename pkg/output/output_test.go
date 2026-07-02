package output

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/a13x22/kube-copy/pkg/copier"
)

func TestPrintJSON_ExcludesSkippedResources(t *testing.T) {
	included := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name": "keep-me",
		},
	}}
	skipped := unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name": "drop-me",
		},
	}}

	results := []copier.CopyResult{
		{Action: "create", Sanitized: &included},
		{Action: "skip", Sanitized: &skipped},
	}

	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := PrintPlan(results, "json")
	w.Close()
	os.Stdout = orig

	if err != nil {
		t.Fatalf("PrintPlan: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "keep-me") {
		t.Errorf("expected included resource in output, got: %s", out)
	}
	if strings.Contains(out, "drop-me") {
		t.Errorf("skipped resource should not appear in output, got: %s", out)
	}
}
