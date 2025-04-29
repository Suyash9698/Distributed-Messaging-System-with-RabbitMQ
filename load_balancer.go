package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var (
	backends     = make([]string, 0)
	backendMutex sync.RWMutex
	current      uint64 = 0
)

// Round-robin load balancing:
func getNextBackend() string {
	backendMutex.RLock()
	defer backendMutex.RUnlock()
	if len(backends) == 0 {
		return ""
	}
	return backends[atomic.AddUint64(&current, 1)%uint64(len(backends))]
}

// Health check for a given backend.
func healthCheck(target string) bool {
	client := http.Client{
		Timeout: 1 * time.Second,
	}
	resp, err := client.Get(target + "/health")
	if err != nil || resp.StatusCode != 200 {
		return false
	}
	return true
}

// Periodically removes dead gateways from the registry.
func startPruneDeadGateways(interval time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			backendMutex.Lock()
			newBackends := make([]string, 0, len(backends))
			for _, b := range backends {
				if healthCheck(b) {
					newBackends = append(newBackends, b)
				} else {
					log.Printf("💀 Removing dead gateway: %s", b)
				}
			}
			backends = newBackends
			backendMutex.Unlock()
		}
	}()
}

// Gateway registration handler.
func registerHandler(w http.ResponseWriter, r *http.Request) {
	address := r.FormValue("address")
	if address == "" {
		http.Error(w, "Missing address", http.StatusBadRequest)
		return
	}
	backendMutex.Lock()
	defer backendMutex.Unlock()
	for _, b := range backends {
		if b == address {
			fmt.Fprintln(w, "Already registered")
			return
		}
	}
	backends = append(backends, address)
	log.Printf("✅ Registered new gateway: %s", address)
	fmt.Fprintln(w, "Registered")
}

// proxyHandler forwards requests to a healthy backend.
func proxyHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("LB got path=%s method=%s", r.URL.Path, r.Method)

	backendMutex.RLock()
	defer backendMutex.RUnlock()
	if len(backends) == 0 {
		http.Error(w, "🚨 No registered gateways", http.StatusServiceUnavailable)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "❌ Failed to read body", http.StatusInternalServerError)
		return
	}
	r.Body.Close()

	maxRetries := len(backends)
	for retries := 0; retries < maxRetries; retries++ {
		target := getNextBackend()
		if target == "" || !healthCheck(target) {
			log.Printf("⚠️ Skipping unhealthy backend: %s", target)
			continue
		}
		proxyReq, err := http.NewRequest(r.Method, target+r.URL.Path, bytes.NewBuffer(bodyBytes))
		if err != nil {
			log.Printf("❌ Failed to create proxy request: %v", err)
			continue
		}
		proxyReq.Header = r.Header.Clone()
		client := &http.Client{}
		resp, err := client.Do(proxyReq)
		if err != nil {
			log.Printf("❌ Gateway unreachable: %v", err)
			continue
		}
		defer resp.Body.Close()
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		log.Printf("✅ Task forwarded to %s and returned %d", target, resp.StatusCode)
		return
	}

	http.Error(w, "🚨 All backends unhealthy", http.StatusServiceUnavailable)
}

func main() {
	rand.Seed(time.Now().UnixNano())
	log.Println("🚀 Go Load Balancer started on :89")

	// Auto-remove dead gateways every 5 seconds
	startPruneDeadGateways(5 * time.Second)

	http.HandleFunc("/register", registerHandler)

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		backendMutex.RLock()
		defer backendMutex.RUnlock()

		for _, target := range backends {
			if healthCheck(target) {
				resp, err := http.Get(fmt.Sprintf("%s/metrics", target))
				if err != nil {
					continue
				}
				defer resp.Body.Close()
				for k, v := range resp.Header {
					w.Header()[k] = v
				}
				w.WriteHeader(resp.StatusCode)
				io.Copy(w, resp.Body)
				return
			}
		}

		http.Error(w, "❌ No healthy gateway found", http.StatusBadGateway)
	})

	http.HandleFunc("/", proxyHandler)
	log.Fatal(http.ListenAndServe(":89", nil))
}
