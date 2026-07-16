package cluster

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	publicv1 "github.com/thetechnick/orlop-gcp-hcp/api/public/v1"
	"github.com/go-logr/logr"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/infra/iam"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/infra/network"
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

func hkVersion(c *publicv1.Cluster) string {
	if c.Spec.Release != nil && c.Spec.Release.Version != "" {
		return c.Spec.Release.Version
	}
	return "<none>"
}

func hkRegion(c *publicv1.Cluster) string {
	if c.Spec.Platform.GCP != nil {
		return c.Spec.Platform.GCP.Region
	}
	return ""
}

func hkClusterStatus(c *publicv1.Cluster) string {
	for _, cond := range c.Status.Conditions {
		if cond.Type == "Ready" && cond.Status == metav1.ConditionTrue {
			return "Ready"
		}
	}
	if len(c.Status.Conditions) == 0 {
		return "Pending"
	}
	return "Progressing"
}

func listClustersHyperkube(cmd *cobra.Command, hkClient client.Client, format string) error {
	list := &publicv1.ClusterList{}
	if err := hkClient.List(cmd.Context(), list); err != nil {
		return fmt.Errorf("listing clusters: %w", err)
	}
	out := cmd.OutOrStdout()

	switch output.ParseFormat(format) {
	case output.FormatJSON:
		return output.PrintJSON(out, list.Items)
	case output.FormatYAML:
		return output.PrintYAML(out, list.Items)
	default:
	}

	if len(list.Items) == 0 {
		fmt.Fprintln(out, "No clusters found.")
		return nil
	}
	t := output.NewTable(out, "NAME", "REGION", "VERSION", "STATUS")
	for i := range list.Items {
		c := &list.Items[i]
		t.AddRow(c.Name, hkRegion(c), hkVersion(c), hkClusterStatus(c))
	}
	return t.Flush()
}

func getClusterHyperkube(cmd *cobra.Command, hkClient client.Client, name, format string) error {
	ns := hkNamespace(cmd)
	c := &publicv1.Cluster{}
	if err := hkClient.Get(cmd.Context(), types.NamespacedName{Namespace: ns, Name: name}, c); err != nil {
		return fmt.Errorf("getting cluster %q: %w", name, err)
	}
	return printHyperkubeCluster(cmd.OutOrStdout(), c, format)
}

func deleteClusterHyperkube(cmd *cobra.Command, hkClient client.Client, name string) error {
	ns := hkNamespace(cmd)
	c := &publicv1.Cluster{}
	c.Name = name
	c.Namespace = ns
	if err := hkClient.Delete(cmd.Context(), c); err != nil {
		return fmt.Errorf("deleting cluster %q: %w", name, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Cluster %s deleted.\n", name)
	return nil
}

func createClusterHyperkube(cmd *cobra.Command, hkClient client.Client, clusterName string, opts *createOptions) error {
	ns := hkNamespace(cmd)

	infraID, err := generateCompliantInfraID(clusterName)
	if err != nil {
		return fmt.Errorf("generating infra ID: %w", err)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Generated infra ID: %s\n", infraID)

	project, _ := cmd.Flags().GetString("project")
	region, _ := cmd.Flags().GetString("region")
	if region == "" {
		region = "us-central1"
	}
	oidcBase, _ := cmd.Flags().GetString("oidc-endpoint")
	if oidcBase == "" {
		return fmt.Errorf("--oidc-endpoint is required (or set GCPHCPCTL_OIDC_ENDPOINT)")
	}

	bpOpts := buildPayloadOptions{
		clusterName:    clusterName,
		infraID:        infraID,
		projectID:      project,
		region:         region,
		endpointAccess: opts.endpointAccess,
		oidcEndpoint:   oidcBase,
		version:        opts.version,
		channelGroup:   opts.channelGroup,
	}

	var iamOut *iam.CreateOutput
	var netOut *network.CreateOutput

	switch {
	case opts.iamConfigFile != "":
		if opts.networkConfigFile == "" {
			return fmt.Errorf("--network-config-file is required with --iam-config-file")
		}
		iamOut, netOut, err = loadHKInfraConfigs(opts.iamConfigFile, opts.networkConfigFile, &bpOpts)
		if err != nil {
			return err
		}
	case opts.setupInfra:
		iamOut, netOut, err = provisionHKInfra(cmd.Context(), cmd.ErrOrStderr(), &bpOpts)
		if err != nil {
			return err
		}
	}

	if ns == "" {
		return fmt.Errorf("--project is required (or set GCPHCPCTL_PROJECT): it is used as the namespace for the hyperkube API")
	}
	if opts.version == "" {
		return fmt.Errorf("--version is required (e.g. --version 4.18.0)")
	}
	if opts.channelGroup == "" {
		return fmt.Errorf("--channel-group is required (e.g. --channel-group stable)")
	}
	c := &publicv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: ns,
		},
		Spec: publicv1.ClusterSpec{
			InfraID:  bpOpts.infraID,
			Platform: publicv1.ClusterPlatformSpec{Type: "GCP"},
		},
	}

	if oidcBase != "" {
		c.Spec.IssuerURL = bpOpts.issuerURL()
	}

	gcp := &publicv1.GCPClusterPlatform{
		EndpointAccess: opts.endpointAccess,
		ProjectID:      bpOpts.projectID,
		Region:         bpOpts.region,
	}

	if iamOut != nil && netOut != nil {
		gcp.Network = netOut.NetworkName
		gcp.Subnet = netOut.SubnetName
		gcp.WorkloadIdentity = &publicv1.WorkloadIdentitySpec{
			PoolID:        iamOut.WorkloadIdentityPool.PoolID,
			ProjectNumber: iamOut.ProjectNumber,
			ProviderID:    iamOut.WorkloadIdentityPool.ProviderID,
			ServiceAccountsRef: &publicv1.ServiceAccountsRef{
				ControlPlaneEmail:    iamOut.ServiceAccounts["ctrlplane-op"],
				NodePoolEmail:        iamOut.ServiceAccounts["nodepool-mgmt"],
				CloudControllerEmail: iamOut.ServiceAccounts["cloud-controller"],
				StorageEmail:         iamOut.ServiceAccounts["gcp-pd-csi"],
				ImageRegistryEmail:   iamOut.ServiceAccounts["image-registry"],
				NetworkEmail:         iamOut.ServiceAccounts["cloud-network"],
			},
		}
	}
	c.Spec.Platform.GCP = gcp

	if opts.version != "" || opts.channelGroup != "" {
		c.Spec.Release = &publicv1.ClusterReleaseSpec{
			Version:      opts.version,
			ChannelGroup: opts.channelGroup,
		}
	}

	if err := hkClient.Create(cmd.Context(), c); err != nil {
		return fmt.Errorf("creating cluster: %w", err)
	}
	return printHyperkubeCluster(cmd.OutOrStdout(), c, opts.outputFmt)
}

func loadHKInfraConfigs(iamConfigFile, networkConfigFile string, bpOpts *buildPayloadOptions) (*iam.CreateOutput, *network.CreateOutput, error) {
	iamData, err := os.ReadFile(iamConfigFile)
	if err != nil {
		return nil, nil, fmt.Errorf("reading IAM config: %w", err)
	}
	var iamOut iam.CreateOutput
	if err := json.Unmarshal(iamData, &iamOut); err != nil {
		return nil, nil, fmt.Errorf("parsing IAM config: %w", err)
	}
	if iamOut.InfraID != "" {
		bpOpts.infraID = iamOut.InfraID
	}
	if bpOpts.projectID == "" {
		bpOpts.projectID = iamOut.ProjectID
	}

	netData, err := os.ReadFile(networkConfigFile)
	if err != nil {
		return nil, nil, fmt.Errorf("reading network config: %w", err)
	}
	var netOut network.CreateOutput
	if err := json.Unmarshal(netData, &netOut); err != nil {
		return nil, nil, fmt.Errorf("parsing network config: %w", err)
	}
	if netOut.Region != "" {
		bpOpts.region = netOut.Region
	}

	return &iamOut, &netOut, nil
}

func provisionHKInfra(ctx context.Context, w io.Writer, bpOpts *buildPayloadOptions) (*iam.CreateOutput, *network.CreateOutput, error) {
	if bpOpts.projectID == "" {
		return nil, nil, fmt.Errorf("--project is required with --setup-infra")
	}
	logger := logr.FromSlogHandler(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))

	fmt.Fprintln(w, ">>> Step 1: Setup IAM infrastructure")
	iamOpts := &iam.CreateOptions{
		ProjectID:     bpOpts.projectID,
		InfraID:       bpOpts.infraID,
		OIDCIssuerURL: bpOpts.issuerURL(),
	}
	iamOut, err := iamOpts.CreateIAM(ctx, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("IAM setup failed: %w", err)
	}
	fmt.Fprintf(w, "  IAM setup complete: %d service accounts created\n", len(iamOut.ServiceAccounts))

	fmt.Fprintln(w, ">>> Step 2: Setup network infrastructure")
	networkOpts := &network.CreateOptions{
		ProjectID: bpOpts.projectID,
		Region:    bpOpts.region,
		InfraID:   bpOpts.infraID,
	}
	netOut, err := networkOpts.CreateNetwork(ctx, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("network setup failed: %w", err)
	}
	fmt.Fprintf(w, "  Network setup complete: VPC=%s, Subnet=%s\n", netOut.NetworkName, netOut.SubnetName)

	return iamOut, netOut, nil
}

func printHyperkubeCluster(w io.Writer, c *publicv1.Cluster, format string) error {
	switch output.ParseFormat(format) {
	case output.FormatJSON:
		return output.PrintJSON(w, c)
	case output.FormatYAML:
		return output.PrintYAML(w, c)
	default:
	}

	bw := bufio.NewWriter(w)

	fmt.Fprintf(bw, "Name:    %s\n", c.Name)
	fmt.Fprintf(bw, "Version: %s\n", hkVersion(c))
	fmt.Fprintf(bw, "Region:  %s\n", hkRegion(c))
	fmt.Fprintf(bw, "Status:  %s\n", hkClusterStatus(c))

	if len(c.Status.Conditions) > 0 {
		fmt.Fprintln(bw, "\nConditions:")
		t := output.NewTable(bw, "TYPE", "STATUS", "REASON", "MESSAGE")
		for _, cond := range c.Status.Conditions {
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
