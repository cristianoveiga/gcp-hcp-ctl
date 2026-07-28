package nodepool

import (
	"bufio"
	"fmt"
	"io"

	publicv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func hkNamespace(cmd *cobra.Command) string {
	project, _ := cmd.Flags().GetString("project")
	return project
}

func hkNPVersion(np *publicv1.NodePool) string {
	if np.Spec.Release.Version != "" {
		return np.Spec.Release.Version
	}
	return "<none>"
}

func hkNPNodeCount(np *publicv1.NodePool) string {
	if np.Spec.NodeCount != nil {
		return fmt.Sprintf("%d", *np.Spec.NodeCount)
	}
	return "-"
}

func hkNPStatus(np *publicv1.NodePool) string {
	for _, cond := range np.Status.Conditions {
		if cond.Type == "Ready" && cond.Status == metav1.ConditionTrue {
			return "Ready"
		}
	}
	if len(np.Status.Conditions) == 0 {
		return "Pending"
	}
	return "Progressing"
}

func listNodePoolsHyperkube(cmd *cobra.Command, hkClient client.Client, clusterRef, format string) error {
	ns := hkNamespace(cmd)
	list := &publicv1.NodePoolList{}
	if err := hkClient.List(cmd.Context(), list, client.InNamespace(ns)); err != nil {
		return fmt.Errorf("listing nodepools: %w", err)
	}

	items := list.Items
	if clusterRef != "" {
		filtered := items[:0]
		for i := range items {
			if items[i].Spec.ClusterID == clusterRef {
				filtered = append(filtered, items[i])
			}
		}
		items = filtered
	}

	out := cmd.OutOrStdout()
	switch output.ParseFormat(format) {
	case output.FormatJSON:
		return output.PrintJSON(out, items)
	case output.FormatYAML:
		return output.PrintYAML(out, items)
	default:
	}

	if len(items) == 0 {
		fmt.Fprintln(out, "No nodepools found.")
		return nil
	}
	t := output.NewTable(out, "NAME", "CLUSTER", "NODES", "VERSION", "STATUS")
	for i := range items {
		np := &items[i]
		t.AddRow(np.Name, np.Spec.ClusterID, hkNPNodeCount(np), hkNPVersion(np), hkNPStatus(np))
	}
	return t.Flush()
}

func getNodePoolHyperkube(cmd *cobra.Command, hkClient client.Client, name, format string) error {
	ns := hkNamespace(cmd)
	np := &publicv1.NodePool{}
	if err := hkClient.Get(cmd.Context(), types.NamespacedName{Namespace: ns, Name: name}, np); err != nil {
		return fmt.Errorf("getting nodepool %q: %w", name, err)
	}
	return printHyperkubeNodePool(cmd.OutOrStdout(), np, format)
}

func deleteNodePoolHyperkube(cmd *cobra.Command, hkClient client.Client, name string) error {
	ns := hkNamespace(cmd)
	np := &publicv1.NodePool{}
	np.Name = name
	np.Namespace = ns
	if err := hkClient.Delete(cmd.Context(), np); err != nil {
		return fmt.Errorf("deleting nodepool %q: %w", name, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Nodepool %s deleted.\n", name)
	return nil
}

func createNodePoolHyperkube(cmd *cobra.Command, hkClient client.Client, npName string, opts *createOptions) error {
	ns := hkNamespace(cmd)
	if ns == "" {
		return fmt.Errorf("--project is required (or set GCPHCPCTL_PROJECT): it is used as the namespace for the hyperkube API")
	}
	if opts.clusterRef == "" {
		return fmt.Errorf("--cluster is required")
	}

	nodeCount := int32(opts.replicas)
	np := &publicv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      npName,
			Namespace: ns,
		},
		Spec: publicv1.NodePoolSpec{
			ClusterID: opts.clusterRef,
			NodeCount: &nodeCount,
			Platform: publicv1.NodePoolPlatformSpec{
				Type: "GCP",
				GCP: &publicv1.GCPNodePoolPlatform{
					MachineType: opts.instanceType,
					DiskSizeGB:  int64(opts.diskSize),
					DiskType:    opts.diskType,
				},
			},
		},
	}

	if opts.zone != "" {
		np.Spec.Platform.GCP.Zone = opts.zone
	}

	if opts.version != "" || opts.channelGroup != "" {
		np.Spec.Release = publicv1.ReleaseSpec{
			Version:      opts.version,
			ChannelGroup: opts.channelGroup,
		}
	}

	if err := hkClient.Create(cmd.Context(), np); err != nil {
		return fmt.Errorf("creating nodepool: %w", err)
	}
	return printHyperkubeNodePool(cmd.OutOrStdout(), np, opts.outputFmt)
}

func scaleNodePoolHyperkube(cmd *cobra.Command, hkClient client.Client, name string, replicas int, format string) error {
	ns := hkNamespace(cmd)
	np := &publicv1.NodePool{}
	if err := hkClient.Get(cmd.Context(), types.NamespacedName{Namespace: ns, Name: name}, np); err != nil {
		return fmt.Errorf("getting nodepool %q: %w", name, err)
	}

	nodeCount := int32(replicas)
	patch := client.MergeFrom(np.DeepCopy())
	np.Spec.NodeCount = &nodeCount
	if err := hkClient.Patch(cmd.Context(), np, patch); err != nil {
		return fmt.Errorf("scaling nodepool %q: %w", name, err)
	}

	if output.ParseFormat(format) != output.FormatText {
		return printHyperkubeNodePool(cmd.OutOrStdout(), np, format)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Nodepool %s scaled to %d nodes.\n", name, replicas)
	return nil
}

func printHyperkubeNodePool(w io.Writer, np *publicv1.NodePool, format string) error {
	switch output.ParseFormat(format) {
	case output.FormatJSON:
		return output.PrintJSON(w, np)
	case output.FormatYAML:
		return output.PrintYAML(w, np)
	default:
	}

	bw := bufio.NewWriter(w)

	fmt.Fprintf(bw, "Name:    %s\n", np.Name)
	fmt.Fprintf(bw, "Cluster: %s\n", np.Spec.ClusterID)
	if np.Spec.NodeCount != nil {
		fmt.Fprintf(bw, "Nodes:   %d\n", *np.Spec.NodeCount)
	}
	if np.Spec.Platform.GCP != nil {
		fmt.Fprintf(bw, "Machine: %s\n", np.Spec.Platform.GCP.MachineType)
	}
	fmt.Fprintf(bw, "Version: %s\n", hkNPVersion(np))
	fmt.Fprintf(bw, "Status:  %s\n", hkNPStatus(np))

	if len(np.Status.Conditions) > 0 {
		fmt.Fprintln(bw, "\nConditions:")
		t := output.NewTable(bw, "TYPE", "STATUS", "REASON", "MESSAGE")
		for _, cond := range np.Status.Conditions {
			msg := cond.Message
			if len(msg) > 80 {
				msg = msg[:80] + "..."
			}
			t.AddRow(string(cond.Type), string(cond.Status), cond.Reason, msg)
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}

	return bw.Flush()
}
