package hyperfleet

import (
	"context"
	"fmt"
	"net/http"

	"golang.org/x/oauth2/google"
)

// NewAPIClient creates a HyperFleet API client authenticated via Application Default Credentials.
// baseURL should point to the HyperFleet API endpoint (e.g. https://hyperfleet-api.example.com).
func NewAPIClient(ctx context.Context, baseURL string) (*Client, error) {
	ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("getting default token source: %w", err)
	}

	injectAuth := func(ctx context.Context, req *http.Request) error {
		token, err := ts.Token()
		if err != nil {
			return fmt.Errorf("getting token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		return nil
	}

	return NewClient(baseURL, WithRequestEditorFn(injectAuth))
}
