package suite

import (
	"context"
	"errors"
	noxmcp "github.com/mapherez/nox-mcp/go"
	"io"
	"net/http"
	"strconv"
)

type Runtime interface {
	Inspect(context.Context, string, string, string) (Deployment, error)
}
type Deployment struct {
	State        string
	Present      bool
	ContainerIDs []string
}

func textNumber(n int) string { return strconv.Itoa(n) }

func readBody(res *http.Response) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(res.Body, noxmcp.SuiteMaxPayloadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > noxmcp.SuiteMaxPayloadBytes {
		return nil, errors.New("Suite payload limit")
	}
	return b, nil
}
