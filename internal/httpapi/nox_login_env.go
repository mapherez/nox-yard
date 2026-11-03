package httpapi

import "os"

func authSocketDirectory() string {
	if value := os.Getenv("NOX_YARD_SUITE_SOCKET_DIR"); value != "" {
		return value
	}
	return "/run/nox-suite"
}
