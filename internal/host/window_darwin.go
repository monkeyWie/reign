package host

import (
	"context"
	"os/exec"
	"strings"
)

func showAuthorizationWindow(ctx context.Context, app string) {
	if strings.HasSuffix(app, ".app") {
		_ = exec.CommandContext(ctx, "/usr/bin/open", app).Run()
	}
}
