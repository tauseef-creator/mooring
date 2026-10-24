// Package containers finds host ports published by Docker and Podman
// containers, using the Docker-compatible Engine API over a Unix socket.
package containers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// Default API sockets, as documented by each engine.
const (
	defaultDockerSocket = "/var/run/docker.sock"
	rootfulPodmanSocket = "/run/podman/podman.sock"
	rootlessPodmanPath  = "podman/podman.sock" // relative to XDG_RUNTIME_DIR
)

// composeDirLabel is set by Docker Compose and podman-compose to the
// directory that holds the compose file.
const composeDirLabel = "com.docker.compose.project.working_dir"

// apiHost fills the host part of request URLs; it is ignored over a Unix socket.
const apiHost = "localhost"

// Container is a running container that publishes ports on the host.
type Container struct {
	ID       string
	Name     string
	Endpoint Endpoint  // the engine running it
	Dir      string    // compose project directory, empty if not from compose
	Started  time.Time // zero if unknown
}

// Endpoint is a container engine's API socket.
type Endpoint struct {
	Runtime  string
	Socket   string // path to the Unix socket
	Explicit bool   // configured by the user, so failures are reported
}

// Endpoints returns the API sockets to query. DOCKER_HOST and CONTAINER_HOST
// override the defaults, as they do for the docker and podman CLIs. Non-Unix
// hosts (tcp://, ssh://) are skipped, since their ports are on another machine.
func Endpoints(getenv func(string) string) []Endpoint {
	var eps []Endpoint
	if host := getenv("DOCKER_HOST"); host != "" {
		if sock, ok := strings.CutPrefix(host, "unix://"); ok {
			eps = append(eps, Endpoint{Runtime: "docker", Socket: sock, Explicit: true})
		}
	} else {
		eps = append(eps, Endpoint{Runtime: "docker", Socket: defaultDockerSocket})
	}

	if host := getenv("CONTAINER_HOST"); host != "" {
		if sock, ok := strings.CutPrefix(host, "unix://"); ok {
			eps = append(eps, Endpoint{Runtime: "podman", Socket: sock, Explicit: true})
		}
	} else {
		if dir := getenv("XDG_RUNTIME_DIR"); dir != "" {
			eps = append(eps, Endpoint{Runtime: "podman", Socket: path.Join(dir, rootlessPodmanPath)})
		}
		eps = append(eps, Endpoint{Runtime: "podman", Socket: rootfulPodmanSocket})
	}
	return eps
}

// Published maps each published TCP host port to its container. Default
// endpoints that cannot be reached are skipped quietly, because the engine
// may simply not be installed or running. Failures on explicit endpoints are
// returned together with whatever the other endpoints found.
func Published(ctx context.Context, eps []Endpoint) (map[uint16]Container, error) {
	byPort := make(map[uint16]Container)
	var errs []error
	for _, ep := range eps {
		found, err := query(ctx, ep)
		if err != nil {
			if ep.Explicit {
				errs = append(errs, fmt.Errorf("%s at %s: %w", ep.Runtime, ep.Socket, err))
			}
			continue
		}
		for port, c := range found {
			byPort[port] = c
		}
	}
	return byPort, errors.Join(errs...)
}

// apiContainer is the subset of a /containers/json entry that we use.
type apiContainer struct {
	ID     string `json:"Id"`
	Names  []string
	Labels map[string]string
	Ports  []struct {
		PublicPort uint16
		Type       string
	}
}

// apiInspect is the subset of a /containers/{id}/json response that we use.
type apiInspect struct {
	State struct {
		StartedAt time.Time
	}
}

// Stop asks the engine to stop the container. Like "docker stop -t", the
// engine sends SIGTERM and then SIGKILL if the container outlives grace.
// ctx must allow longer than grace.
func (c Container) Stop(ctx context.Context, grace time.Duration) error {
	client, done := newClient(c.Endpoint.Socket)
	defer done()

	secs := strconv.Itoa(int(math.Ceil(grace.Seconds())))
	path := "/containers/" + url.PathEscape(c.ID) + "/stop?t=" + secs
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+apiHost+path, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusNotModified: // stopped, or already stopped
		return nil
	default:
		return fmt.Errorf("POST %s: %s", path, resp.Status)
	}
}

// newClient returns an HTTP client that talks to the API over socket, and a
// function that releases its connections.
func newClient(socket string) (*http.Client, func()) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &http.Client{Transport: transport}, transport.CloseIdleConnections
}

func query(ctx context.Context, ep Endpoint) (map[uint16]Container, error) {
	client, done := newClient(ep.Socket)
	defer done()

	var list []apiContainer
	if err := getJSON(ctx, client, "/containers/json", &list); err != nil {
		return nil, err
	}
	found := make(map[uint16]Container)
	for _, ac := range list {
		var ports []uint16
		for _, p := range ac.Ports {
			if p.Type == "tcp" && p.PublicPort != 0 {
				ports = append(ports, p.PublicPort)
			}
		}
		if len(ports) == 0 {
			continue
		}
		c := Container{ID: ac.ID, Endpoint: ep, Dir: ac.Labels[composeDirLabel]}
		if len(ac.Names) > 0 {
			c.Name = strings.TrimPrefix(ac.Names[0], "/")
		}
		var info apiInspect
		if err := getJSON(ctx, client, "/containers/"+url.PathEscape(ac.ID)+"/json", &info); err == nil {
			c.Started = info.State.StartedAt
		}
		for _, port := range ports {
			found[port] = c
		}
	}
	return found, nil
}

func getJSON(ctx context.Context, client *http.Client, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+apiHost+path, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
