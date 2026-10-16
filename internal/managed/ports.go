package managed

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type hostPortClaim struct {
	first, last int
	address     netip.Addr
	protocol    string
	owner       string
}

func (m *Manager) assessPorts(ctx context.Context, name string, model composeModel) error {
	return m.assessServicePorts(ctx, name, "", model)
}

func (m *Manager) assessServicePorts(ctx context.Context, name, service string, model composeModel) error {
	claims, err := desiredHostPorts(model)
	if err != nil || len(claims) == 0 {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return fmt.Errorf("host port availability could not be checked")
	}
	defer cli.Close()
	listed, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return fmt.Errorf("host port availability could not be checked")
	}
	return compareServiceHostPorts(name, service, claims, listed.Items)
}

func desiredHostPorts(model composeModel) ([]hostPortClaim, error) {
	claims := []hostPortClaim{}
	for service := range expectedServices(model) {
		for _, port := range model.Services[service].Ports {
			if port.Published == "" || port.Published == "0" {
				continue // Docker allocates an ephemeral host port.
			}
			firstText, lastText, ranged := strings.Cut(port.Published, "-")
			first, err := strconv.Atoi(firstText)
			last := first
			if err == nil && ranged {
				last, err = strconv.Atoi(lastText)
			}
			if err != nil || first < 1 || last < first || last > 65535 {
				return nil, fmt.Errorf("%w: invalid published port for service %s", ErrInvalidSource, service)
			}
			if first != last {
				continue // Docker may select any free port within the range.
			}
			address := netip.Addr{}
			if port.HostIP != "" {
				address, err = netip.ParseAddr(port.HostIP)
				if err != nil {
					return nil, fmt.Errorf("%w: invalid host address for service %s", ErrInvalidSource, service)
				}
				address = address.Unmap()
			}
			protocol := port.Protocol
			if protocol == "" {
				protocol = "tcp"
			}
			claims = append(claims, hostPortClaim{first, last, address, protocol, service})
		}
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].owner != claims[j].owner {
			return claims[i].owner < claims[j].owner
		}
		return claims[i].first < claims[j].first
	})
	return claims, nil
}

func overlappingHostPorts(a, b hostPortClaim) bool {
	if a.protocol != b.protocol || a.first > b.last || b.first > a.last {
		return false
	}
	if !a.address.IsValid() || !b.address.IsValid() || a.address == b.address {
		return true
	}
	// IPv6 wildcard listeners can be dual stack. Be conservative for ::, but
	// permit distinct specific addresses and IPv4-only versus specific IPv6.
	if a.address.IsUnspecified() {
		return a.address.Is6() || a.address.Is4() == b.address.Is4()
	}
	if b.address.IsUnspecified() {
		return b.address.Is6() || b.address.Is4() == a.address.Is4()
	}
	return false
}

// This detects Docker-published ports, not arbitrary host processes. It is a
// preflight, not a reservation; Docker remains authoritative for binding races.
func compareHostPorts(name string, claims []hostPortClaim, items []container.Summary) error {
	return compareServiceHostPorts(name, "", claims, items)
}

func compareServiceHostPorts(name, service string, claims []hostPortClaim, items []container.Summary) error {
	for index, claim := range claims {
		for _, prior := range claims[:index] {
			if prior.owner != claim.owner && overlappingHostPorts(prior, claim) {
				return fmt.Errorf("%w: host port %d/%s overlaps between services %s and %s", ErrConflict, max(prior.first, claim.first), claim.protocol, prior.owner, claim.owner)
			}
		}
		for _, item := range items {
			if (item.Labels["com.docker.compose.project"] == name && (service == "" || item.Labels["com.docker.compose.service"] == service)) || (item.State != "running" && item.State != "paused" && item.State != "restarting") {
				continue
			}
			for _, port := range item.Ports {
				if port.PublicPort == 0 {
					continue
				}
				actual := hostPortClaim{int(port.PublicPort), int(port.PublicPort), port.IP.Unmap(), port.Type, ""}
				if overlappingHostPorts(claim, actual) {
					owner := item.ID
					if len(item.Names) > 0 {
						names := slices.Clone(item.Names)
						sort.Strings(names)
						owner = strings.TrimPrefix(names[0], "/")
					}
					return fmt.Errorf("%w: host port %d/%s requested by service %s is already published by container %s; keep the existing project port or choose an unused port", ErrConflict, port.PublicPort, claim.protocol, claim.owner, owner)
				}
			}
		}
	}
	return nil
}
