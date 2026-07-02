package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/a13x22/kube-copy/pkg/conflict"
	"github.com/a13x22/kube-copy/pkg/copier"
)

// Operation describes the high-level copy/export intent for the banner.
type Operation struct {
	Kind       string
	Name       string
	SourceNS   string
	TargetNS   string
	TargetName string
	ToContext  string
	ToKubeconfig string
	ToDir      string
	ToFile     string
	Recursive  bool
	Namespaced bool
}

// FormatLocation renders a namespace/name pair for display.
// Cluster-scoped resources show as "name (cluster)".
func FormatLocation(ns, name string, namespaced bool) string {
	if name == "" {
		name = "?"
	}
	if !namespaced || ns == "" {
		return name + " (cluster)"
	}
	return ns + "/" + name
}

// PrintOperationBanner prints a one-line summary of what the command will do.
func PrintOperationBanner(w io.Writer, op Operation) {
	kind := op.Kind
	if kind == "" {
		kind = "Resource"
	}
	src := FormatLocation(op.SourceNS, op.Name, op.Namespaced)
	targetName := op.Name
	if op.TargetName != "" {
		targetName = op.TargetName
	}

	fmt.Fprintln(w)
	switch {
	case op.ToDir != "":
		fmt.Fprintf(w, "  %sExport %s/%s", colorBold, kind, op.Name)
		if op.Recursive {
			fmt.Fprint(w, " (+ dependencies)")
		}
		fmt.Fprintf(w, "%s -> %s\n", colorReset, op.ToDir)
		fmt.Fprintf(w, "  %sfrom %s%s\n", colorGray, src, colorReset)
	case op.ToFile != "":
		fmt.Fprintf(w, "  %sExport %s/%s", colorBold, kind, op.Name)
		if op.Recursive {
			fmt.Fprint(w, " (+ dependencies)")
		}
		fmt.Fprintf(w, "%s -> %s\n", colorReset, op.ToFile)
		fmt.Fprintf(w, "  %sfrom %s%s\n", colorGray, src, colorReset)
	default:
		dst := FormatLocation(op.TargetNS, targetName, op.Namespaced)
		if op.ToContext != "" || op.ToKubeconfig != "" {
			ctx := op.ToContext
			if ctx == "" {
				ctx = "target cluster"
			}
			fmt.Fprintf(w, "  %sCopy %s/%s%s  %s%s -> %s/%s%s\n",
				colorBold, kind, op.Name, colorReset,
				colorGray, src, ctx, dst, colorReset)
		} else {
			fmt.Fprintf(w, "  %sCopy %s/%s%s  %s%s -> %s%s\n",
				colorBold, kind, op.Name, colorReset,
				colorGray, src, dst, colorReset)
		}
	}
}

// PrintConfirmationPrompt shows plan counts and asks to proceed.
func PrintConfirmationPrompt(w io.Writer, results []copier.CopyResult) {
	creates := countAction(results, "create")
	overwrites := countAction(results, "overwrite")
	skips := countAction(results, "skip")
	errors := countErrors(results)

	parts := []string{}
	if creates > 0 {
		parts = append(parts, fmt.Sprintf("%s%d create%s", colorGreen, creates, colorReset))
	}
	if overwrites > 0 {
		parts = append(parts, fmt.Sprintf("%s%d overwrite%s", colorYellow, overwrites, colorReset))
	}
	if skips > 0 {
		parts = append(parts, fmt.Sprintf("%s%d skip%s", colorYellow, skips, colorReset))
	}
	if errors > 0 {
		parts = append(parts, fmt.Sprintf("%s%d error%s", colorRed, errors, colorReset))
	}

	fmt.Fprintf(w, "\n  Proceed with %s? [y/N]: ", strings.Join(parts, ", "))
}

// PrintNothingToDo explains why no resources will be applied.
func PrintNothingToDo(w io.Writer, results []copier.CopyResult) {
	skips := countAction(results, "skip")
	errors := countErrors(results)
	existence, reference := skipReasons(results)

	fmt.Fprintf(w, "\n  %sNothing to do.%s", colorYellow, colorReset)
	if skips > 0 {
		fmt.Fprintf(w, " %d resource(s) skipped", skips)
	}
	if errors > 0 {
		fmt.Fprintf(w, ", %d error(s)", errors)
	}
	fmt.Fprintln(w)

	if existence > 0 {
		fmt.Fprintf(w, "  %sHint:%s %d already exist in target — use %s--on-conflict overwrite%s to replace.\n",
			colorGray, colorReset, existence, colorCyan, colorReset)
	}
	if reference > 0 {
		fmt.Fprintf(w, "  %sHint:%s %d missing dependencies in target — use %s-r%s (recursive) to copy them too.\n",
			colorGray, colorReset, reference, colorCyan, colorReset)
	}
	fmt.Fprintln(w)
}

func skipReasons(results []copier.CopyResult) (existence, reference int) {
	for _, r := range results {
		if r.Action != "skip" {
			continue
		}
		for _, c := range r.Conflicts {
			switch c.Type {
			case conflict.TypeExistence:
				existence++
			case conflict.TypeReference:
				reference++
			}
		}
	}
	return existence, reference
}
