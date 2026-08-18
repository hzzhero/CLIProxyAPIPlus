package cmd

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	log "github.com/sirupsen/logrus"
)

// LoginOptions contains optional settings for the login flow.
type LoginOptions struct {
	NoBrowser    bool
	CallbackPort int
	Metadata     map[string]any
	Prompt       func(string) (string, error)
}

// DoTraeCNLogin handles the Trae CN OAuth login flow using the shared authentication manager.
func DoTraeCNLogin(cfg *config.Config, options *LoginOptions) {
	if options == nil {
		options = &LoginOptions{}
	}

	manager := newAuthManager()

	promptFn := options.Prompt
	if promptFn == nil {
		promptFn = func(prompt string) (string, error) {
			fmt.Println()
			fmt.Println(prompt)
			var value string
			_, err := fmt.Scanln(&value)
			return value, err
		}
	}

	authOpts := &sdkAuth.LoginOptions{
		NoBrowser:    options.NoBrowser,
		CallbackPort: options.CallbackPort,
		Metadata:     options.Metadata,
		Prompt:       promptFn,
	}

	_, savedPath, err := manager.Login(context.Background(), "trae-cn", cfg, authOpts)
	if err != nil {
		log.Errorf("Trae CN login failed: %v", err)
		return
	}

	fmt.Printf("Trae CN authentication successful! Token saved to: %s\n", savedPath)
}
