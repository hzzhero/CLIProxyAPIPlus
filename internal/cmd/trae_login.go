package cmd

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	log "github.com/sirupsen/logrus"
)

// DoTraeCNLogin triggers the browser OAuth polling flow for Trae CN and saves tokens.
// It initiates the OAuth authentication, displays the authorization URL for the
// user to open in their browser, polls until authorization is complete, then
// exchanges the refresh token for a JWT and saves the credentials.
//
// Parameters:
//   - cfg: The application configuration containing proxy and auth directory settings
//   - options: Login options including browser behavior settings
func DoTraeCNLogin(cfg *config.Config, options *LoginOptions) {
	if options == nil {
		options = &LoginOptions{}
	}

	manager := newAuthManager()
	authOpts := &sdkAuth.LoginOptions{
		NoBrowser: options.NoBrowser,
		Metadata:  map[string]string{},
	}

	record, savedPath, err := manager.Login(context.Background(), "trae-cn", cfg, authOpts)
	if err != nil {
		log.Errorf("Trae CN authentication failed: %v", err)
		return
	}

	if savedPath != "" {
		fmt.Printf("Authentication saved to %s\n", savedPath)
	}
	if record != nil && record.Label != "" {
		fmt.Printf("Authenticated as %s\n", record.Label)
	}
	fmt.Println("Trae CN authentication successful!")
}
