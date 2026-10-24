package containers

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const containersJSON = `[
  {"Id": "aaa111", "Names": ["/shop-api-1"], "Image": "shop-api:latest",
   "Labels": {"com.docker.compose.project.working_dir": "/home/me/shop"},
   "Ports": [
     {"IP": "0.0.0.0", "PrivatePort": 8080, "PublicPort": 8080, "Type": "tcp"},
     {"IP": "::", "PrivatePort": 8080, "PublicPort": 8080, "Type": "tcp"},
     {"IP": "0.0.0.0", "PrivatePort": 53, "PublicPort": 5353, "Type": "udp"}
   ]},
  {"Id": "bbb222", "Names": ["/worker"], "Labels": {},
   "Ports": [{"PrivatePort": 9000, "Type": "tcp"}]},
  {"Id": "ccc333", "Names": ["/cache"], "Labels": {},
   "Ports": [{"IP": "127.0.0.1", "PrivatePort": 6379, "PublicPort": 16379, "Type": "tcp"}]}
]`

// fakeEngine serves a minimal Engine API on a Unix socket and returns its path.
func fakeEngine(t *testing.T, handler http.Handler) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "engine.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("cannot listen on a Unix socket: %v", err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return sock
}

func engineAPI() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /containers/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(containersJSON))
	})
	mux.HandleFunc("GET /containers/aaa111/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"State": {"StartedAt": "2026-10-24T09:00:00.5Z"}}`))
	})
	return mux // other inspect calls get 404, leaving Started unknown
}

func TestPublished(t *testing.T) {
	ep := Endpoint{Runtime: "docker", Socket: fakeEngine(t, engineAPI())}
	got, err := Published(context.Background(), []Endpoint{ep})
	if err != nil {
		t.Fatal(err)
	}
	api := Container{
		ID: "aaa111", Name: "shop-api-1", Image: "shop-api:latest", Endpoint: ep, Dir: "/home/me/shop",
		Started: time.Date(2026, 10, 24, 9, 0, 0, 5e8, time.UTC),
	}
	cache := Container{ID: "ccc333", Name: "cache", Endpoint: ep}
	want := map[uint16]Container{8080: api, 16379: cache}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestPublishedErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.sock")
	broken := fakeEngine(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	working := fakeEngine(t, engineAPI())

	tests := []struct {
		name    string
		eps     []Endpoint
		wantErr bool
		wantLen int
	}{
		{"missing default is quiet", []Endpoint{{Runtime: "podman", Socket: missing}}, false, 0},
		{"missing explicit is reported", []Endpoint{{Runtime: "docker", Socket: missing, Explicit: true}}, true, 0},
		{"server error on explicit is reported", []Endpoint{{Runtime: "docker", Socket: broken, Explicit: true}}, true, 0},
		{"one failure keeps other results", []Endpoint{
			{Runtime: "docker", Socket: working},
			{Runtime: "podman", Socket: missing, Explicit: true},
		}, true, 2},
	}
	for _, tt := range tests {
		got, err := Published(context.Background(), tt.eps)
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
		if len(got) != tt.wantLen {
			t.Errorf("%s: got %d ports, want %d", tt.name, len(got), tt.wantLen)
		}
	}
}

func TestStop(t *testing.T) {
	var gotGrace string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /containers/running/stop", func(w http.ResponseWriter, r *http.Request) {
		gotGrace = r.URL.Query().Get("t")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /containers/stopped/stop", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	})
	ep := Endpoint{Runtime: "docker", Socket: fakeEngine(t, mux)}
	ctx := context.Background()

	if err := (Container{ID: "running", Endpoint: ep}).Stop(ctx, 2500*time.Millisecond); err != nil {
		t.Errorf("running: %v", err)
	}
	if gotGrace != "3" {
		t.Errorf("t = %q, want 3 (grace rounded up to whole seconds)", gotGrace)
	}
	if err := (Container{ID: "stopped", Endpoint: ep}).Stop(ctx, time.Second); err != nil {
		t.Errorf("already stopped should succeed, got %v", err)
	}
	if err := (Container{ID: "unknown", Endpoint: ep}).Stop(ctx, time.Second); err == nil {
		t.Error("unknown container: want error, got nil")
	}
}

func TestEndpoints(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []Endpoint
	}{
		{"defaults", map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"}, []Endpoint{
			{Runtime: "docker", Socket: "/var/run/docker.sock"},
			{Runtime: "podman", Socket: "/run/user/1000/podman/podman.sock"},
			{Runtime: "podman", Socket: "/run/podman/podman.sock"},
		}},
		{"no XDG_RUNTIME_DIR", map[string]string{}, []Endpoint{
			{Runtime: "docker", Socket: "/var/run/docker.sock"},
			{Runtime: "podman", Socket: "/run/podman/podman.sock"},
		}},
		{"explicit unix hosts", map[string]string{
			"DOCKER_HOST":    "unix:///home/me/.docker/desktop/docker.sock",
			"CONTAINER_HOST": "unix:///tmp/podman.sock",
		}, []Endpoint{
			{Runtime: "docker", Socket: "/home/me/.docker/desktop/docker.sock", Explicit: true},
			{Runtime: "podman", Socket: "/tmp/podman.sock", Explicit: true},
		}},
		{"remote hosts are skipped", map[string]string{
			"DOCKER_HOST":    "tcp://10.0.0.5:2376",
			"CONTAINER_HOST": "ssh://me@server/run/podman/podman.sock",
		}, nil},
	}
	for _, tt := range tests {
		getenv := func(k string) string { return tt.env[k] }
		if got := Endpoints(getenv); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s:\ngot  %+v\nwant %+v", tt.name, got, tt.want)
		}
	}
}
