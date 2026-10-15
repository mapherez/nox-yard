package managed

import (
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
)

func TestHostPortPreflight(t *testing.T) {
	model := composeModel{Services: map[string]modelService{"app": {Ports: []modelPort{{Published: "8080", Target: 8080}}}}}
	claims, err := desiredHostPorts(model)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, project, state, address, protocol string
		port                                    uint16
		conflict                                bool
	}{
		{"other project's published port", "adguard", "running", "0.0.0.0", "tcp", 8080, true},
		{"own project port", "metro-motion", "running", "0.0.0.0", "tcp", 8080, false},
		{"paused container", "other", "paused", "127.0.0.1", "tcp", 8080, true},
		{"restarting container", "other", "restarting", "::", "tcp", 8080, true},
		{"stopped container", "other", "exited", "0.0.0.0", "tcp", 8080, false},
		{"different host port", "other", "running", "0.0.0.0", "tcp", 2303, false},
		{"different protocol", "other", "running", "0.0.0.0", "udp", 8080, false},
		{"exposed but not published", "other", "running", "0.0.0.0", "tcp", 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			items := []container.Summary{{ID: "owner-id", Names: []string{"/port-owner"}, State: container.ContainerState(test.state),
				Labels: map[string]string{"com.docker.compose.project": test.project},
				Ports:  []container.PortSummary{{IP: netip.MustParseAddr(test.address), PublicPort: test.port, PrivatePort: 80, Type: test.protocol}}}}
			err := compareHostPorts("metro-motion", claims, items)
			if errors.Is(err, ErrConflict) != test.conflict {
				t.Fatalf("unexpected conflict result: %v", err)
			}
			if test.conflict && (!strings.Contains(err.Error(), "8080/tcp") || !strings.Contains(err.Error(), "port-owner")) {
				t.Fatal("conflict must identify the port and owner")
			}
		})
	}
}

func TestHostPortAddressOverlap(t *testing.T) {
	for _, test := range []struct {
		a, b    string
		overlap bool
	}{
		{"127.0.0.1", "127.0.0.2", false}, {"127.0.0.1", "0.0.0.0", true},
		{"127.0.0.1", "::1", false}, {"0.0.0.0", "::1", false},
		{"127.0.0.1", "::", true}, {"::1", "::", true}, {"::1", "::1", true},
	} {
		a := hostPortClaim{8080, 8080, netip.MustParseAddr(test.a), "tcp", "app"}
		b := hostPortClaim{8080, 8080, netip.MustParseAddr(test.b), "tcp", "other"}
		if overlappingHostPorts(a, b) != test.overlap || overlappingHostPorts(b, a) != test.overlap {
			t.Errorf("unexpected address overlap %s / %s", test.a, test.b)
		}
	}
}

func TestHostPortPlans(t *testing.T) {
	zero := 0
	model := composeModel{Services: map[string]modelService{
		"app":      {Ports: []modelPort{{Published: "2303"}, {Published: "0"}, {Target: 8081}, {Published: "2400-2410"}}},
		"profile":  {Profiles: []string{"debug"}, Ports: []modelPort{{Published: "2303"}}},
		"disabled": {Scale: &zero, Ports: []modelPort{{Published: "2303"}}},
	}}
	claims, err := desiredHostPorts(model)
	if err != nil || len(claims) != 1 || claims[0].first != 2303 {
		t.Fatalf("incorrect active fixed ports: %+v %v", claims, err)
	}
	model.Services["other"] = modelService{Ports: []modelPort{{Published: "2303"}}}
	claims, err = desiredHostPorts(model)
	if err != nil || !errors.Is(compareHostPorts("project", claims, nil), ErrConflict) {
		t.Fatal("overlapping services accepted")
	}
	for _, published := range []string{"-1", "65536", "bad", "100-99"} {
		model.Services["app"] = modelService{Ports: []modelPort{{Published: published}}}
		if _, err := desiredHostPorts(model); !errors.Is(err, ErrInvalidSource) {
			t.Errorf("invalid port accepted: %s", published)
		}
	}
}
