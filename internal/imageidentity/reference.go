// Package imageidentity preserves source references when managed deployments
// pin containers to immutable local image IDs.
package imageidentity

const ReferenceLabel = "nox-yard.image-reference"

// Reference is for display and cache-only pulls. Replacement and rollback must
// continue to use the inspected image ID, never this mutable source reference.
func Reference(image string, labels map[string]string) string {
	if reference := labels[ReferenceLabel]; reference != "" {
		return reference
	}
	return image
}
