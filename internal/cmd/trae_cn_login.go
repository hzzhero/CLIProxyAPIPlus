package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
)

// DoTraeCNLogin triggers the Trae CN OAuth flow through the shared
// authentication manager. Trae CN uses the standard authorization-code
// flow with PKCE and a local HTTP callback server (matching cockpit-tools'
// Rust implementation). After a successful token exchange and user profile
// lookup, the credential is persisted via the configured token store.
//
// Parameters:
//   - cfg: The application configuration
//   - options: Login options including NoBrowser, CallbackPort, Prompt
func DoTraeCNLogin(cfg *config.Config, options *LoginOptions) {
	if options == nil {
		options = &LoginOptions{}
	}

	manager := newAuthManager()

	promptFn := options.Prompt
	if promptFn == nil {
		promptFn = func(prompt string) (string, error) {
			fmt.Print(prompt)
			reader := bufio.NewReader(os.Stdin)
			line, err := reader.ReadString('\n')
			if err != nil {
				return "", fmt.Errorf("读取输入失败: %w", err)
			}
			return strings.TrimSpace(line), nil
		}
	}

	authOpts := &sdkAuth.LoginOptions{
		NoBrowser:    options.NoBrowser,
		CallbackPort: options.CallbackPort,
		Prompt:       promptFn,
	}

	ctx := context.Background()
	_, savedPath, err := manager.Login(ctx, "trae-cn", cfg, authOpts)
	if err != nil {
		log.Errorf("Trae CN 登录失败: %v", err)
		return
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Trae CN OAuth 授权完成")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if savedPath != "" {
		fmt.Printf("  凭证文件 : %s\n", savedPath)
	}
	fmt.Println("提示: 该凭证会在启动服务时自动加载，无需重复登录。")
	fmt.Println()
}
