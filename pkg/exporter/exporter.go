// Package exporter writes sanitized CopyResult objects to the local filesystem
// as YAML files, providing an offline destination for kubectl-copy.
//
// Two layouts are supported:
//
//   - Directory mode (ToDir): one YAML file per resource, named
//     "<kind>-<name>.yaml", in the target directory.
//   - File mode (ToFile): a single multi-document YAML file with `---`
//     separators between resources, suitable for `kubectl apply -f`.
package exporter

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/a13x22/kube-copy/pkg/copier"
)

// Options controls how the export is written.
type Options struct {
	// Dir, when set, writes one file per resource into this directory.
	// The directory is created if missing.
	Dir string

	// File, when set, writes a single multi-document YAML to this path.
	// The parent directory must already exist (or be the current dir).
	File string
}

// Progress reports per-file write events. Optional.
type Progress interface {
	Writing(path string)
	Wrote(path string)
}

// Result records the outcome of writing a single resource to disk.
type Result struct {
	Source copier.ResourceRef
	Path   string
	Error  error
}

// Export writes every successfully planned result in `planned` to disk
// according to opts. Results with non-nil Error or no Sanitized payload are
// skipped (their error is preserved for the caller's reporting).
//
// Returns one Result per input result so the caller can render a summary.
func Export(planned []copier.CopyResult, opts Options, prog Progress) ([]Result, error) {
	if opts.Dir == "" && opts.File == "" {
		return nil, fmt.Errorf("exporter: either Dir or File must be set")
	}
	if opts.Dir != "" && opts.File != "" {
		return nil, fmt.Errorf("exporter: Dir and File are mutually exclusive")
	}

	if opts.Dir != "" {
		return exportToDir(planned, opts.Dir, prog)
	}
	return exportToFile(planned, opts.File, prog)
}

func exportToDir(planned []copier.CopyResult, dir string, prog Progress) ([]Result, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory %q: %w", dir, err)
	}

	results := make([]Result, 0, len(planned))
	usedNames := make(map[string]int)

	for _, r := range planned {
		res := Result{Source: r.Source}

		if r.Error != nil {
			res.Error = r.Error
			results = append(results, res)
			continue
		}
		if r.Sanitized == nil {
			res.Error = fmt.Errorf("no sanitized payload to export")
			results = append(results, res)
			continue
		}

		name := fileNameFor(r.Sanitized.GetKind(), r.Sanitized.GetName())
		// Disambiguate duplicates (e.g. ConfigMap/foo in two namespaces).
		if usedNames[name] > 0 {
			name = fmt.Sprintf("%s-%d.yaml", strings.TrimSuffix(name, ".yaml"), usedNames[name])
		}
		usedNames[fileNameFor(r.Sanitized.GetKind(), r.Sanitized.GetName())]++

		path := filepath.Join(dir, name)
		res.Path = path

		if prog != nil {
			prog.Writing(path)
		}

		data, err := yaml.Marshal(r.Sanitized.Object)
		if err != nil {
			res.Error = fmt.Errorf("marshal %s: %w", r.Source.DisplayName(), err)
			results = append(results, res)
			continue
		}

		if err := os.WriteFile(path, data, 0o644); err != nil {
			res.Error = fmt.Errorf("write %s: %w", path, err)
			results = append(results, res)
			continue
		}

		if prog != nil {
			prog.Wrote(path)
		}
		results = append(results, res)
	}

	return results, nil
}

func exportToFile(planned []copier.CopyResult, path string, prog Progress) ([]Result, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create parent directory %q: %w", dir, err)
		}
	}

	if prog != nil {
		prog.Writing(path)
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create %q: %w", path, err)
	}
	defer f.Close()

	results := make([]Result, 0, len(planned))
	wroteAny := false

	for _, r := range planned {
		res := Result{Source: r.Source, Path: path}

		if r.Error != nil {
			res.Error = r.Error
			results = append(results, res)
			continue
		}
		if r.Sanitized == nil {
			res.Error = fmt.Errorf("no sanitized payload to export")
			results = append(results, res)
			continue
		}

		if wroteAny {
			if _, err := io.WriteString(f, "---\n"); err != nil {
				res.Error = fmt.Errorf("write separator: %w", err)
				results = append(results, res)
				continue
			}
		}

		data, err := yaml.Marshal(r.Sanitized.Object)
		if err != nil {
			res.Error = fmt.Errorf("marshal %s: %w", r.Source.DisplayName(), err)
			results = append(results, res)
			continue
		}
		if _, err := f.Write(data); err != nil {
			res.Error = fmt.Errorf("write %s: %w", path, err)
			results = append(results, res)
			continue
		}

		wroteAny = true
		results = append(results, res)
	}

	if prog != nil {
		prog.Wrote(path)
	}

	return results, nil
}

// fileNameFor produces a filesystem-friendly filename for a resource.
// Kubernetes resource names already conform to DNS-1123, so they are safe;
// we only need to lowercase the kind and join with a hyphen.
func fileNameFor(kind, name string) string {
	k := strings.ToLower(kind)
	if k == "" {
		k = "resource"
	}
	if name == "" {
		name = "unnamed"
	}
	return fmt.Sprintf("%s-%s.yaml", k, name)
}
