package executor

import (
	"context"
	"fmt"
	"strings"

	traecn "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/traecn"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func TestTraeCNExecutorIdentifier(t *testing.T) {
	e := NewTraeCNExecutor(nil)
	if got := e.Identifier(); got != "trae-cn" {
		t.Fatalf("Identifier() = %q, want trae-cn", got)
	}
}
