package hyperkube

import (
	"fmt"

	publicv1 "github.com/thetechnick/orlop-gcp-hcp/api/public/v1"
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
// baseURL is the server URL, e.g. "http://localhost:8081".
func NewClient(baseURL string) (client.Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("hyperkube endpoint is required")
	}
	cfg := &rest.Config{Host: baseURL}
	return client.New(cfg, client.Options{Scheme: scheme})
}
