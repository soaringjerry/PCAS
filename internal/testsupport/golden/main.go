// golden runs deterministic external services; PCAS itself stays unmodified.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type rule struct {
	Kind    string `json:"kind"`
	Match   string `json:"match"`
	Content string `json:"content"`
	Delay   int    `json:"delay"`
	Status  int    `json:"status"`
	Once    bool   `json:"once"`
}
type fixture struct {
	sync.Mutex
	Rules   []rule           `json:"rules"`
	Events  []map[string]any `json:"events"`
	Updates []map[string]any `json:"updates"`
}

func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/control" {
		f.Lock()
		defer f.Unlock()
		if r.Method == "POST" {
			var in struct {
				Rules   []rule           `json:"rules"`
				Updates []map[string]any `json:"updates"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				w.WriteHeader(400)
				return
			}
			f.Rules = in.Rules
			f.Updates = append(f.Updates, in.Updates...)
			f.Events = nil
		}
		respond(w, map[string]any{"events": f.Events})
		return
	}
	if r.URL.Path == "/push" {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		f.Lock()
		f.Events = append(f.Events, map[string]any{"kind": "push", "bytes": len(b), "encoding": r.Header.Get("Content-Encoding"), "authorization": r.Header.Get("Authorization") != ""})
		f.Unlock()
		w.WriteHeader(201)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/bot") {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.Lock()
		var result any = true
		if method == "getMe" {
			// Stable identity for the acceptance bot, independent of request
			// ordering and event resets. Match the Bot API User response.
			result = map[string]any{"id": 123456, "is_bot": true, "first_name": "Acceptance fixture", "username": "pcas_acceptance_fixture_bot"}
		} else if method == "getUpdates" {
			updates := []map[string]any{}
			offset, _ := in["offset"].(float64)
			for _, u := range f.Updates {
				if u["update_id"].(float64) >= offset {
					updates = append(updates, u)
				}
			}
			result = updates
		} else {
			in["kind"] = method
			f.Events = append(f.Events, in)
			if method == "sendMessage" {
				id := len(f.Events) + 100
				// Expose the actual response ID to callback-driving tests.
				// Event position is not a Telegram message ID.
				in["messageId"] = id
				result = map[string]any{"message_id": id, "chat": map[string]any{"id": 123, "type": "private"}}
			}
		}
		f.Unlock()
		if method == "getUpdates" {
			select {
			case <-time.After(200 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
		respond(w, map[string]any{"ok": true, "result": result})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/chat/completions") {
		var in struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			w.WriteHeader(400)
			return
		}
		system, prompt := "", ""
		for _, m := range in.Messages {
			if m.Role == "system" {
				system = m.Content
			} else {
				prompt = m.Content
			}
		}
		kind := "assistant"
		if strings.Contains(system, "前台秘书") {
			kind = "secretary"
			if i := strings.LastIndex(prompt, "这句话："); i >= 0 {
				prompt = strings.TrimSpace(prompt[i+len("这句话："):])
			}
		} else if strings.Contains(system, "从原文提取独立线索") {
			kind = "extraction"
		} else if strings.Contains(system, "摘要") {
			kind = "summary"
		}
		selected := rule{Content: "- [ ] 核对资料\n- [ ] 写出方案\n- [ ] 复核提交"}
		if kind == "extraction" {
			selected.Content = `{"items":[]}`
		}
		if kind == "secretary" {
			selected.Content = `{"reply":"收到。","actions":[]}`
		}
		f.Lock()
		f.Events = append(f.Events, map[string]any{"kind": "model", "role": kind, "prompt": prompt})
		for i, r := range f.Rules {
			if r.Kind == kind && strings.Contains(prompt, r.Match) {
				selected = r
				if r.Once {
					f.Rules = append(f.Rules[:i], f.Rules[i+1:]...)
				}
				break
			}
		}
		f.Unlock()
		if selected.Delay > 0 {
			select {
			case <-time.After(time.Duration(selected.Delay) * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
		if selected.Status > 0 {
			w.WriteHeader(selected.Status)
			respond(w, map[string]any{"error": map[string]string{"message": "fixture upstream timeout", "code": "fixture_error"}})
			return
		}
		// RunAgents now asks for output/used JSON. Keep the original fixture
		// text and error/call sequencing, wrapping only this deputy protocol.
		if kind == "assistant" && strings.Contains(system, "output为完整建议或草稿") {
			body, _ := json.Marshal(map[string]any{"output": selected.Content, "used": []any{}})
			selected.Content = string(body)
		}
		respond(w, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": selected.Content}}}, "usage": map[string]int{"prompt_tokens": 100, "completion_tokens": 20}})
		return
	}
	http.NotFound(w, r)
}
func main() {
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0700); err != nil {
		panic(err)
	}
	f := &fixture{}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	defer server.Close()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "PCAS isolated acceptance"}, DNSNames: []string{"api.telegram.org", "push.pcas.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, _ := x509.MarshalECPrivateKey(key)
	pair, _ := tls.X509KeyPair(certPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	caPath := filepath.Join(dir, "ca.pem")
	if err = os.WriteFile(caPath, certPEM, 0600); err != nil {
		panic(err)
	}
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	upstream.StartTLS()
	defer upstream.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || (r.Host != "api.telegram.org:443" && r.Host != "push.pcas.test:443") {
			http.Error(w, "external network disabled", 403)
			return
		}
		target, err := net.Dial("tcp", upstream.Listener.Addr().String())
		if err != nil {
			w.WriteHeader(502)
			return
		}
		client, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			target.Close()
			return
		}
		_, _ = buf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buf.Flush()
		go func() { defer client.Close(); defer target.Close(); _, _ = io.Copy(target, client) }()
		go func() { defer client.Close(); defer target.Close(); _, _ = io.Copy(client, target) }()
	}))
	defer proxy.Close()
	models := map[string]any{"extraction_provider": "golden", "providers": []any{map[string]any{"id": "golden", "name": "验收假模型", "model": "golden", "protocol": "openai", "base_url": server.URL, "cost_mode": "free", "max_output_tokens": 2000}}}
	b, _ := json.Marshal(models)
	if err = os.WriteFile(filepath.Join(dir, "models.json"), b, 0600); err != nil {
		panic(err)
	}
	env := fmt.Sprintf("export PCAS_TEST_FIXTURE_URL=%q\nexport PCAS_MODELS_FILE=%q\nexport SSL_CERT_FILE=%q\nexport HTTPS_PROXY=%q\nexport HTTP_PROXY=%q\nexport NO_PROXY=127.0.0.1,localhost\n", server.URL, filepath.Join(dir, "models.json"), caPath, proxy.URL, proxy.URL)
	if err = os.WriteFile(filepath.Join(dir, "fixture.env"), []byte(env), 0600); err != nil {
		panic(err)
	}
	fmt.Println("isolated model, Telegram and push fixtures ready")
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)
	<-done
}
