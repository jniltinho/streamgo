package mediamtx_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core/output"
	"github.com/jniltinho/streamgo/internal/mediamtx"
)

type request struct {
	method, path, auth, contentType, body string
}

func fake(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *[]request) {
	t.Helper()
	var mu sync.Mutex
	var got []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		u, p, _ := r.BasicAuth()
		mu.Lock()
		got = append(got, request{r.Method, r.URL.RequestURI(), u + ":" + p, r.Header.Get("Content-Type"), string(b)})
		mu.Unlock()
		handle(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func client(t *testing.T, srv *httptest.Server) *mediamtx.Client {
	t.Helper()
	c, err := mediamtx.New(srv.URL, srv.URL+"/", "streamgo", "pw")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPublish(t *testing.T) {
	t.Parallel()
	srv, got := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/program/B1/whip/5b9b947b")
		w.Header().Set("ETag", "*")
		w.Header().Add("Link", `<stun:stun.example>; rel="ice-server"`)
		w.Header().Set("Content-Type", "application/sdp")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("v=0 answer"))
	})
	c := client(t, srv)
	a, err := c.Publish(context.Background(), "B1", []byte("v=0 offer"))
	if err != nil {
		t.Fatal(err)
	}
	if string(a.SDP) != "v=0 answer" || a.Resource != srv.URL+"/program/B1/whip/5b9b947b" || a.ETag != "*" || len(a.Links) != 1 {
		t.Fatalf("%+v", a)
	}
	r := (*got)[0]
	if r.method != http.MethodPost || r.path != "/program/B1/whip" || r.auth != "streamgo:pw" || r.contentType != "application/sdp" || r.body != "v=0 offer" {
		t.Fatalf("%+v", r)
	}
	if err := c.Close(context.Background(), a.Resource); err != nil {
		t.Fatal(err)
	}
	if r := (*got)[1]; r.method != http.MethodDelete || r.path != "/program/B1/whip/5b9b947b" || r.auth != "streamgo:pw" {
		t.Fatalf("%+v", r)
	}
	// a stored URL can only name the WHIP server's own resources
	for _, bad := range []string{"http://other.example/program/B1/whip/x", srv.URL + "/v3/config/global/patch"} {
		if err := c.Close(context.Background(), bad); err == nil {
			t.Fatalf("closed %s", bad)
		}
	}
	if len(*got) != 2 {
		t.Fatal("a foreign resource was called")
	}
}

func TestPublishRefused(t *testing.T) {
	t.Parallel()
	for name, handle := range map[string]http.HandlerFunc{
		"refused": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) },
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://elsewhere.example/whip", http.StatusTemporaryRedirect)
		},
		"no location":      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) },
		"foreign location": func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Location", "http://evil.example/x"); w.WriteHeader(http.StatusCreated) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, got := fake(t, handle)
			_, err := client(t, srv).Publish(context.Background(), "B1", []byte("o"))
			if !errors.Is(err, output.ErrUpstream) {
				t.Fatalf("error %v", err)
			}
			if len(*got) != 1 {
				t.Fatal("a redirect was followed")
			}
		})
	}
	t.Run("no answer", func(t *testing.T) {
		t.Parallel()
		srv, _ := fake(t, func(http.ResponseWriter, *http.Request) { time.Sleep(time.Second) })
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if _, err := client(t, srv).Publish(ctx, "B1", []byte("o")); !errors.Is(err, output.ErrUpstream) {
			t.Fatalf("error %v", err)
		}
	})
	if mediamtx.Timeout != 10*time.Second {
		t.Fatal("the media server's timeout is not 10 s")
	}
}

func TestControlAPI(t *testing.T) {
	t.Parallel()
	srv, got := fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v3/paths/get/program/B1":
			_, _ = w.Write([]byte(`{"name":"program/B1","source":{"type":"webRTCSession","id":"3c7cc4a1"},"readers":[]}`))
		case r.URL.Path == "/v3/paths/get/program/B2":
			_, _ = w.Write([]byte(`{"name":"program/B2","source":{"type":"rtspSession","id":"x"}}`))
		case strings.HasPrefix(r.URL.Path, "/v3/paths/get/"):
			http.NotFound(w, r)
		case r.URL.Path == "/v3/paths/list":
			_, _ = w.Write([]byte(`{"itemCount":3,"items":[
				{"name":"program/B1","source":{"type":"webRTCSession","id":"s1"}},
				{"name":"air/B1","source":{"type":"rtspSession","id":"s2"}},
				{"name":"program/B3","source":null}]}`))
		case r.URL.Path == "/v3/webrtcsessions/kick/s1":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	})
	c := client(t, srv)
	ctx := context.Background()
	if s, err := c.Publisher(ctx, "B1"); err != nil || s != "3c7cc4a1" {
		t.Fatalf("%q %v", s, err)
	}
	for _, id := range []string{"B2", "none"} {
		if s, err := c.Publisher(ctx, id); err != nil || s != "" {
			t.Fatalf("%s: %q %v", id, s, err)
		}
	}
	m, err := c.Publishing(ctx)
	if err != nil || len(m) != 1 || m["B1"] != "s1" {
		t.Fatalf("%v %v", m, err)
	}
	if err := c.Kick(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if err := c.Kick(ctx, "gone"); err != nil {
		t.Fatal("a session already gone:", err)
	}
	if err := c.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	for _, r := range *got {
		if r.auth != "streamgo:pw" {
			t.Fatalf("%s without the internal credentials", r.path)
		}
	}
	if r := (*got)[4]; r.method != http.MethodPost || r.path != "/v3/webrtcsessions/kick/s1" {
		t.Fatalf("%+v", r)
	}
}
