package hyperkube

import (
	"fmt"

	publicv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var scheme = runtime.NewScheme()

func init() {
	if err := publicv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

// NewClient builds a controller-runtime typed client pointed at the hyperkube API server.
// baseURL is the server URL, e.g. "https://hyperkube.example.com" or "http://localhost:8081".
// Set insecure to true to skip TLS certificate verification (useful for local development).
func NewClient(baseURL string, insecure bool) (client.Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("hyperkube endpoint is required")
	}
	cfg := &rest.Config{Host: baseURL}
	if insecure {
		cfg.TLSClientConfig = rest.TLSClientConfig{Insecure: true}
	}
	return client.New(cfg, client.Options{Scheme: scheme})
}
