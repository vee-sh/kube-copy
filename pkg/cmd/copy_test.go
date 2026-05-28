package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestNoArgs_PrintsHelpInsteadOfError(t *testing.T) {
	cmd := NewCopyCommand()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error for zero args, got: %v", err)
	}

	got := out.String()
	for _, want := range []string{"Usage:", "kubectl copy", "--to-dir"} {
		if !strings.Contains(got, want) {
			t.Errorf("help output missing %q\n---\n%s", want, got)
		}
	}
}

func TestTooManyArgs_StillRejected(t *testing.T) {
	cmd := NewCopyCommand()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"deployment", "myapp", "extra"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when more than 2 args are passed")
	}
}

// newTestOptions returns Options pre-populated with the same defaults cobra
// would assign via flag registration. SourceNamespace is also pre-set so
// Complete() doesn't try to read the user's kubeconfig.
func newTestOptions() *Options {
	return &Options{
		SourceNamespace: "src",
		OnConflict:      "skip",
		Output:          "table",
	}
}

func TestComplete_ParsesSlashArg(t *testing.T) {
	o := newTestOptions()
	o.ToNamespace = "dst" // different ns so the same-ns guard does not trip
	if err := o.Complete(nil, []string{"deployment/myapp"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.ResourceKind != "deployment" || o.ResourceName != "myapp" {
		t.Errorf("parsed (%q, %q), want (deployment, myapp)", o.ResourceKind, o.ResourceName)
	}
}

func TestComplete_ParsesSpaceSeparatedArgs(t *testing.T) {
	o := newTestOptions()
	o.ToNamespace = "dst"
	if err := o.Complete(nil, []string{"Deployment", "myapp"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.ResourceKind != "deployment" {
		t.Errorf("ResourceKind should be lowercased: got %q", o.ResourceKind)
	}
	if o.ResourceName != "myapp" {
		t.Errorf("ResourceName: got %q, want myapp", o.ResourceName)
	}
}

func TestComplete_RejectsMalformedSlashArg(t *testing.T) {
	cases := []string{"deployment", "deployment/", "/myapp", ""}
	for _, arg := range cases {
		o := newTestOptions()
		err := o.Complete(nil, []string{arg})
		if err == nil {
			t.Errorf("expected error for arg %q", arg)
		}
	}
}

func TestComplete_DefaultsTargetNamespaceToSource(t *testing.T) {
	o := newTestOptions()
	o.ToName = "myapp-copy" // satisfy the same-namespace guard
	if err := o.Complete(nil, []string{"deployment/myapp"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.ToNamespace != "src" {
		t.Errorf("ToNamespace should default to SourceNamespace: got %q", o.ToNamespace)
	}
}

func TestComplete_SameNamespaceRequiresRename(t *testing.T) {
	o := newTestOptions()
	// No ToName, no ToContext, no ToKubeconfig, no export -> should fail.
	err := o.Complete(nil, []string{"deployment/myapp"})
	if err == nil {
		t.Fatal("expected error when copying within same namespace without --to-name")
	}
	if !strings.Contains(err.Error(), "--to-name") {
		t.Errorf("error message should mention --to-name: %v", err)
	}
}

func TestComplete_SameNamespaceAllowedForExport(t *testing.T) {
	// Exporting to disk does not collide with anything in the source namespace,
	// so the same-name same-namespace rule should not apply.
	o := newTestOptions()
	o.ToDir = "./out"
	if err := o.Complete(nil, []string{"deployment/myapp"}); err != nil {
		t.Fatalf("export-to-dir in same namespace should be allowed: %v", err)
	}
}

func TestComplete_RejectsToDirAndToFileTogether(t *testing.T) {
	o := newTestOptions()
	o.ToDir = "./out"
	o.ToFile = "./bundle.yaml"
	err := o.Complete(nil, []string{"deployment/myapp"})
	if err == nil {
		t.Fatal("expected error when both --to-dir and --to-file are set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should mention mutual exclusion: %v", err)
	}
}

func TestComplete_RejectsExportWithToContext(t *testing.T) {
	o := newTestOptions()
	o.ToDir = "./out"
	o.ToContext = "prod"
	err := o.Complete(nil, []string{"deployment/myapp"})
	if err == nil {
		t.Fatal("expected error when --to-dir combined with --to-context")
	}
}

func TestComplete_RejectsExportWithToKubeconfig(t *testing.T) {
	o := newTestOptions()
	o.ToFile = "./bundle.yaml"
	o.ToKubeconfig = "/tmp/kubeconfig"
	err := o.Complete(nil, []string{"deployment/myapp"})
	if err == nil {
		t.Fatal("expected error when --to-file combined with --to-kubeconfig")
	}
}

func TestComplete_RejectsExportWithDryRun(t *testing.T) {
	o := newTestOptions()
	o.ToDir = "./out"
	o.DryRun = true
	err := o.Complete(nil, []string{"deployment/myapp"})
	if err == nil {
		t.Fatal("expected error when --to-dir combined with --dry-run")
	}
	if !strings.Contains(err.Error(), "dry-run") {
		t.Errorf("error should mention --dry-run: %v", err)
	}
}

func TestComplete_RejectsBadOnConflict(t *testing.T) {
	o := newTestOptions()
	o.ToNamespace = "dst"
	o.OnConflict = "explode"
	err := o.Complete(nil, []string{"deployment/myapp"})
	if err == nil {
		t.Fatal("expected error for invalid --on-conflict value")
	}
}

func TestComplete_AcceptsValidOnConflictValues(t *testing.T) {
	for _, v := range []string{"skip", "warn", "overwrite"} {
		o := newTestOptions()
		o.ToNamespace = "dst"
		o.OnConflict = v
		if err := o.Complete(nil, []string{"deployment/myapp"}); err != nil {
			t.Errorf("on-conflict=%q should be accepted: %v", v, err)
		}
	}
}

func TestComplete_RejectsBadOutput(t *testing.T) {
	o := newTestOptions()
	o.ToNamespace = "dst"
	o.Output = "xml"
	err := o.Complete(nil, []string{"deployment/myapp"})
	if err == nil {
		t.Fatal("expected error for invalid --output value")
	}
}

func TestComplete_AcceptsValidOutputValues(t *testing.T) {
	for _, v := range []string{"table", "yaml", "json"} {
		o := newTestOptions()
		o.ToNamespace = "dst"
		o.Output = v
		if err := o.Complete(nil, []string{"deployment/myapp"}); err != nil {
			t.Errorf("output=%q should be accepted: %v", v, err)
		}
	}
}

func TestIsExport(t *testing.T) {
	cases := []struct {
		name string
		o    Options
		want bool
	}{
		{"empty", Options{}, false},
		{"to-namespace only", Options{ToNamespace: "x"}, false},
		{"to-dir set", Options{ToDir: "./out"}, true},
		{"to-file set", Options{ToFile: "./bundle.yaml"}, true},
	}
	for _, c := range cases {
		if got := c.o.isExport(); got != c.want {
			t.Errorf("%s: isExport() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTargetName(t *testing.T) {
	o := &Options{ResourceName: "myapp"}
	if got := o.TargetName(); got != "myapp" {
		t.Errorf("default target name should be source: got %q", got)
	}
	o.ToName = "myapp-v2"
	if got := o.TargetName(); got != "myapp-v2" {
		t.Errorf("override target name: got %q, want myapp-v2", got)
	}
}
