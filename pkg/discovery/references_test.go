package discovery

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestExtractForwardRefs_StorageClassFromPVC(t *testing.T) {
	pvc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "PersistentVolumeClaim",
			"metadata": map[string]interface{}{
				"name":      "my-pvc",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"storageClassName": "fast-ssd",
			},
		},
	}

	refs := extractForwardRefs(pvc, "default")
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}

	ref := refs[0]
	if ref.Namespaced {
		t.Error("expected Namespaced false for StorageClass")
	}
	if ref.Namespace != "" {
		t.Errorf("expected empty namespace, got %q", ref.Namespace)
	}
	if ref.Name != "fast-ssd" {
		t.Errorf("expected name fast-ssd, got %q", ref.Name)
	}
	wantGVR := schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}
	if ref.GVR != wantGVR {
		t.Errorf("expected GVR %v, got %v", wantGVR, ref.GVR)
	}
}
