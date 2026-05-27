package exporter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/a13x22/kube-copy/pkg/copier"
)

// makeResult is a small helper that constructs a CopyResult carrying a
// minimal unstructured object suitable for export round-tripping.
func makeResult(kind, name, namespace string) copier.CopyResult {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       kind,
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
	}}
	return copier.CopyResult{
		Source: copier.ResourceRef{
			Kind:      kind,
			Name:      name,
			Namespace: namespace,
		},
		TargetName: name,
		TargetNS:   namespace,
		Action:     "create",
		Sanitized:  obj,
	}
}

// recordingProgress captures Writing/Wrote calls so tests can assert on them.
type recordingProgress struct {
	writing []string
	wrote   []string
}

func (r *recordingProgress) Writing(path string) { r.writing = append(r.writing, path) }
func (r *recordingProgress) Wrote(path string)   { r.wrote = append(r.wrote, path) }

func TestExport_NoTargetSet(t *testing.T) {
	_, err := Export(nil, Options{}, nil)
	if err == nil {
		t.Fatal("expected error when neither Dir nor File is set")
	}
}

func TestExport_BothTargetsSet(t *testing.T) {
	_, err := Export(nil, Options{Dir: "x", File: "y"}, nil)
	if err == nil {
		t.Fatal("expected error when both Dir and File are set")
	}
}

func TestExport_Dir_WritesOneFilePerResource(t *testing.T) {
	dir := t.TempDir()
	planned := []copier.CopyResult{
		makeResult("Deployment", "myapp", "default"),
		makeResult("ConfigMap", "myapp-config", "default"),
		makeResult("Service", "myapp", "default"),
	}

	prog := &recordingProgress{}
	results, err := Export(planned, Options{Dir: dir}, prog)
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}

	wantFiles := []string{
		"deployment-myapp.yaml",
		"configmap-myapp-config.yaml",
		"service-myapp.yaml",
	}
	for _, name := range wantFiles {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s to exist: %v", path, err)
		}
	}

	for _, r := range results {
		if r.Error != nil {
			t.Errorf("unexpected error for %s: %v", r.Source.DisplayName(), r.Error)
		}
		if r.Path == "" {
			t.Errorf("expected Path to be set for %s", r.Source.DisplayName())
		}
	}

	if len(prog.writing) != 3 || len(prog.wrote) != 3 {
		t.Errorf("progress not reported for every file: writing=%d wrote=%d", len(prog.writing), len(prog.wrote))
	}
}

func TestExport_Dir_CreatesMissingDirectory(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "deep", "nested", "out")

	planned := []copier.CopyResult{makeResult("Deployment", "myapp", "default")}

	if _, err := Export(planned, Options{Dir: dir}, nil); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected directory to be created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected created path to be a directory")
	}
}

func TestExport_Dir_LowercasesKindInFilename(t *testing.T) {
	dir := t.TempDir()
	planned := []copier.CopyResult{
		makeResult("HorizontalPodAutoscaler", "myapp-hpa", "default"),
	}

	results, err := Export(planned, Options{Dir: dir}, nil)
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}

	want := filepath.Join(dir, "horizontalpodautoscaler-myapp-hpa.yaml")
	if results[0].Path != want {
		t.Errorf("path mismatch: got %s, want %s", results[0].Path, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected file %s: %v", want, err)
	}
}

func TestExport_Dir_DisambiguatesDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	planned := []copier.CopyResult{
		makeResult("ConfigMap", "shared", "ns-a"),
		makeResult("ConfigMap", "shared", "ns-b"),
		makeResult("ConfigMap", "shared", "ns-c"),
	}

	results, err := Export(planned, Options{Dir: dir}, nil)
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}

	paths := []string{results[0].Path, results[1].Path, results[2].Path}
	want := []string{
		filepath.Join(dir, "configmap-shared.yaml"),
		filepath.Join(dir, "configmap-shared-1.yaml"),
		filepath.Join(dir, "configmap-shared-2.yaml"),
	}
	for i, p := range paths {
		if p != want[i] {
			t.Errorf("paths[%d]: got %s, want %s", i, p, want[i])
		}
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file %s: %v", p, err)
		}
	}
}

func TestExport_Dir_PreservesErroredResults(t *testing.T) {
	dir := t.TempDir()
	planned := []copier.CopyResult{
		makeResult("Deployment", "ok", "default"),
		{
			Source: copier.ResourceRef{Kind: "Secret", Name: "fetch-failed", Namespace: "default"},
			Error:  errors.New("fetch failed"),
		},
		{
			Source:    copier.ResourceRef{Kind: "ConfigMap", Name: "no-payload", Namespace: "default"},
			Sanitized: nil,
		},
	}

	results, err := Export(planned, Options{Dir: dir}, nil)
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}

	if results[0].Error != nil {
		t.Errorf("first result should have no error, got %v", results[0].Error)
	}
	if results[1].Error == nil {
		t.Error("second result should propagate fetch error")
	}
	if results[2].Error == nil {
		t.Error("third result should report missing payload")
	}

	// Only the first file should exist.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected 1 file written, got %d", len(entries))
	}
}

func TestExport_Dir_FileContentsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	planned := []copier.CopyResult{makeResult("Deployment", "myapp", "staging")}

	results, err := Export(planned, Options{Dir: dir}, nil)
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}

	data, err := os.ReadFile(results[0].Path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	body := string(data)

	for _, want := range []string{"apiVersion: v1", "kind: Deployment", "name: myapp", "namespace: staging"} {
		if !strings.Contains(body, want) {
			t.Errorf("written YAML missing %q\n---\n%s", want, body)
		}
	}
}

func TestExport_File_SingleResourceHasNoLeadingSeparator(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.yaml")

	planned := []copier.CopyResult{makeResult("Deployment", "myapp", "default")}

	if _, err := Export(planned, Options{File: path}, nil); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if strings.HasPrefix(string(data), "---") {
		t.Errorf("single-resource bundle should not start with separator:\n%s", string(data))
	}
}

func TestExport_File_MultiResourceUsesSeparators(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.yaml")

	planned := []copier.CopyResult{
		makeResult("Deployment", "myapp", "default"),
		makeResult("Service", "myapp", "default"),
		makeResult("ConfigMap", "myapp-config", "default"),
	}

	if _, err := Export(planned, Options{File: path}, nil); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	body := string(data)

	// Two separators between three documents.
	if got := strings.Count(body, "\n---\n"); got != 2 {
		t.Errorf("expected 2 document separators, got %d\n---\n%s", got, body)
	}

	// All three kinds should appear.
	for _, kind := range []string{"kind: Deployment", "kind: Service", "kind: ConfigMap"} {
		if !strings.Contains(body, kind) {
			t.Errorf("bundle missing %q", kind)
		}
	}
}

func TestExport_File_CreatesParentDirectory(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "deep", "nested", "bundle.yaml")

	planned := []copier.CopyResult{makeResult("Deployment", "myapp", "default")}

	if _, err := Export(planned, Options{File: path}, nil); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected bundle at %s: %v", path, err)
	}
}

func TestExport_File_SkipsErroredAndContinues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.yaml")

	planned := []copier.CopyResult{
		makeResult("Deployment", "ok", "default"),
		{
			Source: copier.ResourceRef{Kind: "Secret", Name: "bad", Namespace: "default"},
			Error:  errors.New("fetch failed"),
		},
		makeResult("ConfigMap", "also-ok", "default"),
	}

	results, err := Export(planned, Options{File: path}, nil)
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if results[1].Error == nil {
		t.Error("errored result should be preserved")
	}

	data, _ := os.ReadFile(path)
	body := string(data)

	if !strings.Contains(body, "kind: Deployment") || !strings.Contains(body, "kind: ConfigMap") {
		t.Errorf("bundle should contain both successful resources:\n%s", body)
	}
	if strings.Contains(body, "name: bad") {
		t.Errorf("bundle should not contain the errored resource:\n%s", body)
	}
	// Exactly one separator (between the two successful documents).
	if got := strings.Count(body, "\n---\n"); got != 1 {
		t.Errorf("expected 1 document separator, got %d\n---\n%s", got, body)
	}
}

func TestFileNameFor(t *testing.T) {
	cases := []struct {
		kind, name, want string
	}{
		{"Deployment", "myapp", "deployment-myapp.yaml"},
		{"HorizontalPodAutoscaler", "hpa-1", "horizontalpodautoscaler-hpa-1.yaml"},
		{"", "orphan", "resource-orphan.yaml"},
		{"Service", "", "service-unnamed.yaml"},
	}
	for _, c := range cases {
		got := fileNameFor(c.kind, c.name)
		if got != c.want {
			t.Errorf("fileNameFor(%q, %q) = %q, want %q", c.kind, c.name, got, c.want)
		}
	}
}
