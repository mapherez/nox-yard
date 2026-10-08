package managed

import (
	"errors"
	"testing"
)

func TestGlobalEnvironmentCannotChangeComposeOrDockerControls(t *testing.T) {
	source := "services:\n  web:\n    image: alpine:3.23\n"
	for _, value := range []string{"COMPOSE_PROFILES=unhealthy\n", "export DOCKER_HOST=tcp://other-host:2375\n", "'COMPOSE_FILE' : other.yml\n"} {
		_, _, err := prepareCompose(source, map[string]string{".env": value})
		if !errors.Is(err, ErrInvalidSource) {
			t.Fatalf("global control accepted: %v", err)
		}
	}
	_, cleanup, err := prepareCompose(source+"    env_file: app.env\n", map[string]string{"app.env": "DOCKER_HOST=application-value\n"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
}

func TestImplicitEnvironmentFileIsListedAndCanSupplyInterpolation(t *testing.T) {
	source := "services:\n  web:\n    image: alpine:3.23\n    command: [echo, '${VALUE}']\n"
	info, err := InspectSource(source)
	if err != nil || len(info.EnvFiles) != 1 || info.EnvFiles[0].Path != ".env" || info.EnvFiles[0].Required {
		t.Fatalf("implicit environment file missing: %+v %v", info, err)
	}
	_, cleanup, err := prepareCompose(source, map[string]string{".env": "VALUE=application-value\n"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
}
