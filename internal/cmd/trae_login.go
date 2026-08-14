package cmd

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	log "github.com/sirupsen/logrus"
)

// DoTraeLogin triggers the OAuth2 authorization code flow for Trae IDE and saves tokens.
func DoTraeLogin(cfg *config.Config, options *LoginOptions) {
	if options == nil {
		options = &LoginOptions{}
	}

	manager := newAuthManager()
	authOpts := &sdkAuth.LoginOptions{
		NoBrowser:    options.NoBrowser,
		Metadata:     map[string]string{},
		Prompt:       options.Prompt,
		CallbackPort: options.CallbackPort,
	}

	record, savedPath, err := manager.Login(context.Background(), "trae", cfg, authOpts)
	if err != nil {
		log.Errorf("Trae authentication failed: %v", err)
		return
	}

	if savedPath != "" {
		fmt.Printf("Authentication saved to %s\n", savedPath)
	}
	if record != nil && record.Label != "" {
		fmt.Printf("Authenticated as %s\n", record.Label)
	}
	fmt.Println("Trae authentication successful!")
}
