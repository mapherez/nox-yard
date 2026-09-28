package managed

import (
	"fmt"
	"os"
	"path"
	"strings"
	"unicode"
)

func NormalizeProjectsBase(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "~" || strings.HasPrefix(value, "~/") {
		home := os.Getenv("NOX_HOST_HOME")
		if !path.IsAbs(home) {
			home = os.Getenv("NOX_HOST_USERPROFILE")
		}
		if home == "" {
			return "", fmt.Errorf("%w: host home is unavailable; enter an absolute path on the Docker host", ErrInvalidSource)
		}
		if value == "~" {
			value = home
		} else {
			value = strings.TrimRight(home, "/\\") + "/" + strings.TrimPrefix(value, "~/")
		}
	}
	if len(value) >= 3 && unicode.IsLetter(rune(value[0])) && value[1] == ':' && (value[2] == '/' || value[2] == '\\') {
		drive := strings.ToLower(value[:1])
		rest := strings.ReplaceAll(value[3:], "\\", "/")
		for _, component := range strings.Split(rest, "/") {
			if component == ".." {
				return "", fmt.Errorf("%w: host path must not traverse directories", ErrInvalidSource)
			}
		}
		value = "/run/desktop/mnt/host/" + drive + "/" + rest
	}
	if !path.IsAbs(value) || strings.ContainsAny(value, "\x00\r\n\\:") {
		return "", fmt.Errorf("%w: enter an absolute directory on the Docker host", ErrInvalidSource)
	}
	return path.Clean(value), nil
}
