package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	log "github.com/sirupsen/logrus"
)

// DoTraeCNLogin handles the Trae CN browser OAuth login using the shared
// authentication manager. It opens the Trae CN authorization page in a
// browser (with a local callback server and manual paste fallback) and saves
// the resulting tokens to the configured auth directory.
//
// Parameters:
//   - cfg: The application configuration
//   - options: Login options including browser behavior and prompts
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
		Metadata:     map[string]string{},
		Prompt:       promptFn,
	}

	_, savedPath, err := manager.Login(context.Background(), "trae-cn", cfg, authOpts)
	if err != nil {
		if emailErr, ok := errors.AsType[*sdkAuth.EmailRequiredError](err); ok {
			log.Error(emailErr.Error())
			return
		}
		fmt.Printf("Trae CN authentication failed: %v\n", err)
		return
	}

	if savedPath != "" {
		fmt.Printf("Authentication saved to %s\n", savedPath)
	}

	fmt.Println("Trae CN authentication successful!")
}
