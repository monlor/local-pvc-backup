package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// FilterOptions represents the common filtering options for all commands
type FilterOptions struct {
	Node      string
	Namespace string
	PVC       string
	All       bool
}

// AddFilterFlags adds the common filter flags to a command
func AddFilterFlags(cmd *cobra.Command, opts *FilterOptions) {
	cmd.Flags().StringVar(&opts.Node, "node", "", "Filter by node name")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Filter by namespace")
	cmd.Flags().StringVar(&opts.PVC, "pvc", "", "Filter by PVC name (format: name or namespace/name)")
	cmd.Flags().BoolVar(&opts.All, "all", false, "Select all backup-enabled PVCs")
}

// Validate validates the filter options
func (f *FilterOptions) Validate() error {
	// If --all is specified, other filters should not be used
	if f.All && (f.Node != "" || f.Namespace != "" || f.PVC != "") {
		return fmt.Errorf("--all cannot be used with other filter options")
	}

	// If no filters specified, require at least one
	if !f.All && f.Node == "" && f.Namespace == "" && f.PVC == "" {
		return fmt.Errorf("must specify at least one filter option (--all, --node, --namespace, or --pvc)")
	}

	// If PVC is specified with namespace/name format, extract namespace
	if f.PVC != "" && strings.Contains(f.PVC, "/") {
		parts := strings.SplitN(f.PVC, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid PVC format, use: namespace/pvc-name")
		}
		
		// If namespace is also specified separately, check for conflicts
		if f.Namespace != "" && f.Namespace != parts[0] {
			return fmt.Errorf("conflicting namespace: --namespace=%s but PVC specifies %s", f.Namespace, parts[0])
		}
		
		// Extract namespace and PVC name
		f.Namespace = parts[0]
		f.PVC = parts[1]
	}

	// If PVC is specified without namespace, require namespace
	if f.PVC != "" && f.Namespace == "" {
		return fmt.Errorf("PVC name requires namespace (use --namespace or namespace/pvc-name format)")
	}

	return nil
}

// String returns a human-readable representation of the filter
func (f *FilterOptions) String() string {
	if f.All {
		return "all backup-enabled PVCs"
	}

	var parts []string
	if f.Node != "" {
		parts = append(parts, fmt.Sprintf("node=%s", f.Node))
	}
	if f.Namespace != "" {
		parts = append(parts, fmt.Sprintf("namespace=%s", f.Namespace))
	}
	if f.PVC != "" {
		parts = append(parts, fmt.Sprintf("pvc=%s", f.PVC))
	}

	if len(parts) == 0 {
		return "no filters"
	}

	return strings.Join(parts, ", ")
}

// Matches checks if a PVC matches the filter criteria
func (f *FilterOptions) Matches(nodeName, namespace, pvcName string) bool {
	if f.All {
		return true
	}

	// Check node filter
	if f.Node != "" && f.Node != nodeName {
		return false
	}

	// Check namespace filter
	if f.Namespace != "" && f.Namespace != namespace {
		return false
	}

	// Check PVC filter
	if f.PVC != "" && f.PVC != pvcName {
		return false
	}

	return true
}