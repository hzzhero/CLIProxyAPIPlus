package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	log "github.com/sirupsen/logrus"
)

// DoQoderCNLogin handles the Qoder CN PAT-based login using the shared
// authentication manager. Qoder CN does not support the browser device flow;
// the user must supply a Personal Access Token (pt-...) obtained from
// https://qoder.com.cn/account/integrations. The PAT is exchanged for a
// short-lived job token and persisted alongside the PAT so the scheduler can
// re-exchange it before expiry.
//
// Parameters:
//   - cfg: The application configuration
//   - options: Login options including the interactive PAT prompt
func DoQoderCNLogin(cfg *config.Config, options *LoginOptions) {
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

	_, savedPath, err := manager.Login(context.Background(), "qoder-cn", cfg, authOpts)
	if err != nil {
		if emailErr, ok := errors.AsType[*sdkAuth.EmailRequiredError](err); ok {
			log.Error(emailErr.Error())
			return
		}
		fmt.Printf("Qoder CN authentication failed: %v\n", err)
		return
	}

	if savedPath != "" {
		fmt.Printf("Authentication saved to %s\n", savedPath)
	}

	fmt.Println("Qoder CN authentication successful!")
}
