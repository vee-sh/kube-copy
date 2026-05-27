package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/a13x22/kube-copy/pkg/client"
	"github.com/a13x22/kube-copy/pkg/copier"
	"github.com/a13x22/kube-copy/pkg/discovery"
	"github.com/a13x22/kube-copy/pkg/exporter"
	"github.com/a13x22/kube-copy/pkg/output"
)

// Options holds all flags and parsed arguments for the copy command.
type Options struct {
	// Source identification
	SourceKubeconfig string
	SourceContext    string
	SourceNamespace  string
	ResourceArg      string // raw argument like "deployment/myapp"

	// Parsed from ResourceArg
	ResourceKind string
	ResourceName string

	// Target overrides
	ToNamespace  string
	ToName       string
	ToContext    string
	ToKubeconfig string

	// Filesystem export target (mutually exclusive with cluster targets).
	ToDir  string // write one YAML file per resource into this directory
	ToFile string // write all resources as a single multi-doc YAML file

	// Behavior flags
	Recursive  bool
	DryRun     bool
	Yes        bool   // skip confirmation prompt
	Quiet      bool   // suppress progress output
	OnConflict string // "skip", "warn", "overwrite"
	Output     string // "table", "yaml", "json"
}

// isExport reports whether this invocation targets the local filesystem
// instead of a live Kubernetes cluster.
func (o *Options) isExport() bool {
	return o.ToDir != "" || o.ToFile != ""
}

// NewCopyCommand creates the root cobra command for kubectl-copy.
func NewCopyCommand() *cobra.Command {
	o := &Options{}

	cmd := &cobra.Command{
		Use:   "copy <resource>/<name> [flags]",
		Short: "Copy Kubernetes resources across namespaces or clusters",
		Long: `Copy Kubernetes resources intelligently, sanitizing metadata and
detecting conflicts to avoid broken or duplicate resources.

Supports copying single resources or entire dependency graphs with --recursive.
Works across namespaces (same cluster) and across clusters (different context/kubeconfig).

Resource can be specified as:
  deployment/myapp              slash-separated
  deployment myapp              space-separated
  deployment.apps/myapp         kubectl-style with API group
  deploy/myapp                  short alias`,
		Example: `  # Copy a deployment to another namespace
  kubectl copy deployment/myapp --to-namespace staging
  kubectl copy deployment myapp --to-namespace staging

  # Copy with a new name in same namespace
  kubectl copy deployment/myapp --to-name myapp-v2

  # Copy to another cluster
  kubectl copy deployment/myapp --to-context prod-cluster --to-namespace default

  # Recursive copy (includes related ConfigMaps, Secrets, Services, etc.)
  kubectl copy deployment/myapp --to-namespace staging -r

  # Dry-run to preview what would happen
  kubectl copy deployment/myapp --to-namespace staging -r --dry-run

  # Export the resource and its dependencies to YAML files on disk
  kubectl copy deployment/myapp -r --to-dir ./manifests

  # Export everything to a single multi-doc YAML file
  kubectl copy deployment/myapp -r --to-file ./bundle.yaml

  # Skip confirmation prompt
  kubectl copy deployment/myapp --to-namespace staging -y`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.RangeArgs(1, 2),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return o.Complete(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run()
		},
	}

	// Source flags (standard kubectl flags)
	cmd.Flags().StringVar(&o.SourceKubeconfig, "kubeconfig", "", "path to the kubeconfig file")
	cmd.Flags().StringVar(&o.SourceContext, "context", "", "kubeconfig context to use for the source")
	cmd.Flags().StringVarP(&o.SourceNamespace, "namespace", "n", "", "source namespace (defaults to current context namespace)")

	// Target flags
	cmd.Flags().StringVar(&o.ToNamespace, "to-namespace", "", "target namespace (defaults to source namespace)")
	cmd.Flags().StringVar(&o.ToNamespace, "to-ns", "", "target namespace (alias for --to-namespace)")
	cmd.Flags().StringVar(&o.ToName, "to-name", "", "new resource name (required for same-namespace copy)")
	cmd.Flags().StringVar(&o.ToContext, "to-context", "", "target kubeconfig context (for cross-cluster copy)")
	cmd.Flags().StringVar(&o.ToKubeconfig, "to-kubeconfig", "", "target kubeconfig file (for cross-cluster copy)")
	cmd.Flags().StringVar(&o.ToDir, "to-dir", "", "export resources to YAML files in this directory (one file per resource)")
	cmd.Flags().StringVar(&o.ToFile, "to-file", "", "export all resources as a single multi-doc YAML file")

	// Behavior flags
	cmd.Flags().BoolVarP(&o.Recursive, "recursive", "r", false, "copy the full dependency graph")
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", false, "preview what would be copied without making changes")
	cmd.Flags().BoolVarP(&o.Yes, "yes", "y", false, "skip confirmation prompt")
	cmd.Flags().BoolVarP(&o.Quiet, "quiet", "q", false, "suppress progress output")
	cmd.Flags().StringVar(&o.OnConflict, "on-conflict", "skip", "conflict strategy: skip, warn, overwrite")
	cmd.Flags().StringVarP(&o.Output, "output", "o", "table", "output format: table, yaml, json")

	return cmd
}

// Complete parses and validates the command arguments.
func (o *Options) Complete(cmd *cobra.Command, args []string) error {
	// Support both "resource/name" and "resource name" formats
	if len(args) == 2 {
		// Space-separated: "deployment myapp"
		o.ResourceKind = strings.ToLower(args[0])
		o.ResourceName = args[1]
	} else {
		o.ResourceArg = args[0]
		// Parse resource/name
		parts := strings.SplitN(o.ResourceArg, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid resource argument %q: expected <resource>/<name> or <resource> <name>", o.ResourceArg)
		}
		o.ResourceKind = strings.ToLower(parts[0])
		o.ResourceName = parts[1]
	}

	// Note: we do NOT strip the ".group" suffix here (e.g. "deployment.apps").
	// The REST mapper handles it natively during resolution.

	// Default source namespace
	if o.SourceNamespace == "" {
		o.SourceNamespace = getDefaultNamespace(o.SourceKubeconfig, o.SourceContext)
	}

	// Default target namespace to source namespace
	if o.ToNamespace == "" {
		o.ToNamespace = o.SourceNamespace
	}

	// Validate export-target mutual exclusion.
	if o.ToDir != "" && o.ToFile != "" {
		return fmt.Errorf("--to-dir and --to-file are mutually exclusive")
	}
	if o.isExport() && (o.ToContext != "" || o.ToKubeconfig != "") {
		return fmt.Errorf("--to-dir/--to-file cannot be combined with --to-context or --to-kubeconfig")
	}
	if o.isExport() && o.DryRun {
		return fmt.Errorf("--dry-run has no effect with --to-dir/--to-file; the export itself is the output")
	}

	// Validate: same namespace + no rename = conflict (for namespaced resources).
	// Skip this check when exporting to disk -- the files are independent of any
	// live cluster, so a same-name same-namespace export is meaningful.
	if !o.isExport() && o.ToNamespace == o.SourceNamespace && o.ToName == "" && o.ToContext == "" && o.ToKubeconfig == "" {
		return fmt.Errorf("copying within the same namespace requires --to-name to avoid name collision")
	}

	// Validate on-conflict
	switch o.OnConflict {
	case "skip", "warn", "overwrite":
	default:
		return fmt.Errorf("invalid --on-conflict value %q: must be skip, warn, or overwrite", o.OnConflict)
	}

	// Validate output
	switch o.Output {
	case "table", "yaml", "json":
	default:
		return fmt.Errorf("invalid --output value %q: must be table, yaml, or json", o.Output)
	}

	return nil
}

// TargetName returns the target resource name, falling back to the source name.
func (o *Options) TargetName() string {
	if o.ToName != "" {
		return o.ToName
	}
	return o.ResourceName
}

// Run executes the copy operation with plan/apply flow.
func (o *Options) Run() error {
	ctx := context.TODO()

	// Set up progress reporter
	prog := output.NewProgress(o.Quiet)

	// Build clients
	prog.Connecting()
	clients, err := client.New(o.SourceKubeconfig, o.SourceContext, o.ToKubeconfig, o.ToContext)
	if err != nil {
		prog.Clear()
		return fmt.Errorf("cannot connect to cluster: %w\n    Check your kubeconfig and network connectivity.", err)
	}

	// Resolve resource type dynamically via the API server's discovery
	// This handles short names, plural, singular, CRDs, resource.group format, etc.
	resolved, err := clients.Resolve(o.ResourceKind)
	if err != nil {
		prog.Clear()
		return err
	}

	primaryRef := copier.ResourceRef{
		GVR:        resolved.GVR,
		Kind:       resolved.Kind,
		Name:       o.ResourceName,
		Namespace:  o.SourceNamespace,
		Namespaced: resolved.Namespaced,
	}
	if !resolved.Namespaced {
		primaryRef.Namespace = ""
	}

	// Cluster-scoped in same cluster requires --to-name to avoid overwriting.
	// Skipped for export-to-disk -- no live target to collide with.
	if !primaryRef.Namespaced && !o.isExport() && o.ToName == "" && o.ToContext == "" && o.ToKubeconfig == "" {
		return fmt.Errorf("copying a cluster-scoped resource (e.g. StorageClass) in the same cluster requires --to-name")
	}

	// Build list of resources to copy
	refs := []copier.ResourceRef{primaryRef}

	if o.Recursive {
		prog.Discovering()
		discovered, err := discovery.Discover(ctx, clients.SourceDynamic, primaryRef.GVR, primaryRef.Name, primaryRef.Namespace)
		if err != nil {
			prog.Clear()
			return fmt.Errorf("discovering dependencies: %w", err)
		}
		refs = append(refs, discovered...)
		prog.DiscoveredCount(len(discovered))
	}

	// Create copier. In export mode we deliberately leave TargetClient nil so
	// that Plan skips target-cluster conflict detection.
	c := &copier.Copier{
		SourceClient: clients.SourceDynamic,
		OnConflict:   o.OnConflict,
		Progress:     prog,
	}
	if !o.isExport() {
		c.TargetClient = clients.TargetDynamic
	}

	// Target namespace is empty for cluster-scoped resources.
	toNamespace := o.ToNamespace
	if !primaryRef.Namespaced {
		toNamespace = ""
	}

	// Phase 1: Plan (fetch, sanitize, and conflict-detect when not exporting).
	planned := c.PlanAll(ctx, refs, toNamespace, o.ToName)
	prog.Clear()

	// Export mode short-circuits the apply/confirm flow.
	if o.isExport() {
		return runExport(planned, o, prog)
	}

	// Show the plan
	if o.DryRun {
		return output.PrintPlan(planned, o.Output)
	}

	// Show plan table and ask for confirmation (unless --yes)
	output.PrintPlan(planned, "table")

	if !o.Yes {
		// Check if any actionable work exists
		hasWork := false
		for _, r := range planned {
			if r.Error == nil && r.Action != "skip" {
				hasWork = true
				break
			}
		}
		if !hasWork {
			fmt.Fprintf(os.Stderr, "\n  Nothing to do.\n\n")
			return nil
		}

		if !askConfirmation() {
			fmt.Fprintf(os.Stderr, "  Aborted.\n\n")
			return nil
		}
	}

	// Phase 2: Apply
	fmt.Fprintln(os.Stderr)
	c.ApplyAll(ctx, planned)

	// Show results
	return output.PrintResults(planned, o.Output)
}

// runExport writes the planned resources to the local filesystem according
// to the export flags. It prints a summary of what was written to stderr and
// surfaces any per-resource errors via a non-zero exit.
func runExport(planned []copier.CopyResult, o *Options, prog *output.ProgressReporter) error {
	results, err := exporter.Export(planned, exporter.Options{
		Dir:  o.ToDir,
		File: o.ToFile,
	}, prog)
	if err != nil {
		return err
	}

	var wrote, failed int
	for _, r := range results {
		if r.Error != nil {
			failed++
			fmt.Fprintf(os.Stderr, "  ERROR %s: %v\n", r.Source.DisplayName(), r.Error)
			continue
		}
		wrote++
		if o.ToDir != "" {
			fmt.Fprintf(os.Stderr, "  + %s -> %s\n", r.Source.DisplayName(), r.Path)
		}
	}

	target := o.ToDir
	if target == "" {
		target = o.ToFile
	}
	fmt.Fprintf(os.Stderr, "\n  Exported %d resource(s) to %s", wrote, target)
	if failed > 0 {
		fmt.Fprintf(os.Stderr, ", %d error(s)", failed)
	}
	fmt.Fprintln(os.Stderr)

	if failed > 0 {
		return fmt.Errorf("export completed with %d error(s)", failed)
	}
	return nil
}

// askConfirmation prompts the user for y/N confirmation on stderr.
func askConfirmation() bool {
	fmt.Fprintf(os.Stderr, "  Proceed? [y/N]: ")
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}

// getDefaultNamespace returns the namespace from the current kubeconfig context.
func getDefaultNamespace(kubeconfig, context string) string {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if context != "" {
		overrides.CurrentContext = context
	}
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	ns, _, err := cfg.Namespace()
	if err != nil || ns == "" {
		return "default"
	}
	return ns
}
