// Throwaway spike server: static pages, LiveKit room+token, and the WHIP proxy of design D3.
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	lkURL     = "http://127.0.0.1:17880"
	lkKey     = "spikekey"
	lkSecret  = "spike-throwaway-secret-0123456789abcdef"
	whipBase  = "http://127.0.0.1:18889"
	mtxUser   = "streamgo"
	mtxPass   = "spike-internal-pass"
	storeFile = "../out/whip-sessions.json" // survives a restart of this handler
)

var (
	mu       sync.Mutex
	upstream = &http.Client{Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
)

func jwt(claims map[string]any) string {
	enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	s := enc(map[string]string{"alg": "HS256", "typ": "JWT"}) + "." + enc(claims)
	m := hmac.New(sha256.New, []byte(lkSecret))
	m.Write([]byte(s))
	return s + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func grant(id string, video map[string]any) string {
	now := time.Now().Unix()
	return jwt(map[string]any{"iss": lkKey, "sub": id, "nbf": now - 10, "exp": now + 6*3600, "video": video})
}

// GET /token?room=bc_x&id=host-1 : creates the room (auto_create is off), then issues a join token.
func token(w http.ResponseWriter, r *http.Request) {
	room, id := r.URL.Query().Get("room"), r.URL.Query().Get("id")
	body, _ := json.Marshal(map[string]any{"name": room, "empty_timeout": 300})
	req, _ := http.NewRequest("POST", lkURL+"/twirp/livekit.RoomService/CreateRoom", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+grant("streamgo", map[string]any{"roomCreate": true}))
	res, err := upstream.Do(req)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		http.Error(w, string(b), 502)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"url": "ws://127.0.0.1:17880",
		"token": grant(id, map[string]any{"room": room, "roomJoin": true, "canPublish": true, "canSubscribe": true})})
}

func load() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(storeFile); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

func save(m map[string]string) error {
	b, _ := json.Marshal(m)
	return os.WriteFile(storeFile, b, 0o600)
}

func forward(method, target string, body io.Reader, hdr http.Header) (*http.Response, []byte, error) {
	req, _ := http.NewRequest(method, target, body)
	for _, h := range []string{"Content-Type", "If-Match"} { // never the client's cookies or Authorization
		if v := hdr.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	req.SetBasicAuth(mtxUser, mtxPass)
	res, err := upstream.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	return res, b, err
}

func whipPost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mu.Lock()
	defer mu.Unlock()
	m := load()
	if old := m[id]; old != "" { // one ingest session per broadcast
		if res, _, err := forward("DELETE", old, nil, http.Header{}); err == nil {
			log.Printf("deleted previous session %s: %d", old, res.StatusCode)
		}
	}
	offer, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		http.Error(w, "offer too large", 413)
		return
	}
	target := whipBase + "/program/" + url.PathEscape(id) + "/whip"
	res, b, err := forward("POST", target, bytes.NewReader(offer), r.Header)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	if res.StatusCode != 201 {
		log.Printf("upstream POST %d: %s", res.StatusCode, b)
		http.Error(w, "upstream refused", 502)
		return
	}
	loc, _ := res.Location()
	m[id] = loc.String()
	if err := save(m); err != nil {
		forward("DELETE", loc.String(), nil, http.Header{})
		http.Error(w, "store failed", 502)
		return
	}
	log.Printf("POST %s -> %s", id, loc)
	for _, h := range []string{"ETag", "Link"} {
		for _, v := range res.Header.Values(h) {
			w.Header().Add(h, v)
		}
	}
	w.Header().Set("Content-Type", "application/sdp")
	w.Header().Set("Location", "/b/"+id+"/program/whip/session")
	w.WriteHeader(201)
	w.Write(b)
}

func whipSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mu.Lock()
	defer mu.Unlock()
	m := load()
	target := m[id]
	if target == "" {
		http.Error(w, "no session", 404)
		return
	}
	res, b, err := forward(r.Method, target, http.MaxBytesReader(w, r.Body, 64<<10), r.Header)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	log.Printf("%s %s -> %d", r.Method, target, res.StatusCode)
	if r.Method == "DELETE" {
		delete(m, id)
		save(m)
	}
	w.WriteHeader(res.StatusCode)
	w.Write(b)
}

func main() {
	http.HandleFunc("POST /log", func(w http.ResponseWriter, r *http.Request) { // page logs (pages run without DevTools)
		b, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		name := filepath.Base(r.URL.Query().Get("name"))
		if f, err := os.OpenFile("../out/pagelogs/"+name+".log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			f.Write(append(b, '\n'))
			f.Close()
		}
	})
	http.HandleFunc("GET /token", token)
	http.HandleFunc("POST /b/{id}/program/whip", whipPost)
	http.HandleFunc("PATCH /b/{id}/program/whip/session", whipSession)
	http.HandleFunc("DELETE /b/{id}/program/whip/session", whipSession)
	http.Handle("GET /lk/", http.StripPrefix("/lk/", http.FileServer(http.Dir("../node_modules/livekit-client/dist"))))
	http.Handle("GET /", http.FileServer(http.Dir("../web")))
	fmt.Println("listening on 127.0.0.1:18080")
	log.Fatal(http.ListenAndServe("127.0.0.1:18080", nil))
}
