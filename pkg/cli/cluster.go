package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/hyperfleet"
	"github.com/spf13/cobra"
)

func newClusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage HyperFleet clusters",
	}
	cmd.AddCommand(newClusterCreateCmd())
	return cmd
}

func newClusterCreateCmd() *cobra.Command {
	var (
		name      string
		projectID string
		region    string
		network   string
		subnet    string
		apiURL    string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a HyperFleet cluster",
		Example: `  # Create a cluster in us-central1
  gcphcpctl cluster create --name my-cluster --project-id my-project --region us-central1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			client, err := hyperfleet.NewAPIClient(ctx, apiURL)
			if err != nil {
				return fmt.Errorf("creating hyperfleet client: %w", err)
			}

			gcpPlatform := hyperfleet.ClusterPlatform{
				ProjectID: &projectID,
				Region:    &region,
			}
			if network != "" {
				gcpPlatform.Network = &network
			}
			if subnet != "" {
				gcpPlatform.Subnet = &subnet
			}

			kind := "Cluster"
			body := hyperfleet.PostClusterJSONRequestBody{
				Kind: &kind,
				Name: name,
				Spec: hyperfleet.ClusterSpec{
					Platform: hyperfleet.ClusterPlatformSpec{
						Type: hyperfleet.GCP,
						Gcp:  gcpPlatform,
					},
				},
			}

			fmt.Fprintf(os.Stderr, "Creating cluster %q in project %s/%s...\n", name, projectID, region)

			resp, err := client.PostCluster(ctx, body)
			if err != nil {
				return fmt.Errorf("creating cluster: %w", err)
			}
			defer resp.Body.Close()

			return printResponse(cmd, resp)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Cluster name (required)")
	cmd.Flags().StringVar(&projectID, "project-id", "", "GCP project ID (required)")
	cmd.Flags().StringVar(&region, "region", "", "GCP region (required)")
	cmd.Flags().StringVar(&network, "network", "", "VPC network name")
	cmd.Flags().StringVar(&subnet, "subnet", "", "VPC subnet name")
	cmd.Flags().StringVar(&apiURL, "api-url", os.Getenv("HYPERFLEET_API_URL"), "HyperFleet API base URL (env: HYPERFLEET_API_URL)")

	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("project-id")
	_ = cmd.MarkFlagRequired("region")

	return cmd
}

func printResponse(cmd *cobra.Command, resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	outputFormat, _ := cmd.Flags().GetString("output")
	if outputFormat == "json" {
		fmt.Println(string(body))
		return nil
	}

	var cluster hyperfleet.Cluster
	if err := json.Unmarshal(body, &cluster); err != nil {
		fmt.Println(string(body))
		return nil
	}

	fmt.Printf("NAME\tCREATED\n")
	fmt.Printf("%s\t%s\n", cluster.Name, cluster.CreatedTime.String())
	return nil
}
