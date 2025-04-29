package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func randomID() string {
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("corr-%d", rand.Int())
}

func main() {

	resp, err := http.Get("http://localhost:9000/generate-id")
	if err != nil {
		fmt.Println("❌ Error:", err)
		return
	}
	defer resp.Body.Close()

	var g struct {
		UserID string `json:"userId"`
	}
	json.NewDecoder(resp.Body).Decode(&g)
	userId := g.UserID

	fmt.Printf("🌐 Opening login URL for userId=%s\n", userId)
	loginURL := fmt.Sprintf("http://localhost:9000/login?uid=%s", userId)
	openBrowser(loginURL)

	fmt.Println("📢 Please complete Google login in browser...")

	var token string
	for {

		verify := fmt.Sprintf("http://localhost:9000/fetch-token?userId=%s", userId)
		r, err := http.Get(verify)
		if err == nil && r.StatusCode == 200 {
			body, _ := io.ReadAll(r.Body)
			token = string(body)
			r.Body.Close()
			if token != "" {
				break
			}
		}
		time.Sleep(2 * time.Second)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("🔑 Paste the JWT token you copied from browser: ")
	tokenRaw, _ := reader.ReadString('\n')
	token = strings.TrimSpace(tokenRaw)

	verifyReq, _ := http.NewRequest("GET", "http://localhost:9000/verify-token", nil)
	verifyReq.Header.Set("Authorization", "Bearer "+token)

	verifyResp, err := http.DefaultClient.Do(verifyReq)
	if err != nil {
		fmt.Println("❌ Could not connect to Auth Server:", err)
		os.Exit(1)
	}
	defer verifyResp.Body.Close()

	if verifyResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(verifyResp.Body)
		fmt.Println("❌ Token verification failed:", string(body))
		os.Exit(1)
	}

	fmt.Println("✅ Token verified! Proceeding to submit task...")

	fmt.Print("📝 Enter task to submit: ")
	taskRaw, _ := reader.ReadString('\n')
	task := strings.TrimSpace(taskRaw)

	fmt.Print("🔁 Enter routing key (leave blank if not needed): ")
	routingKeyRaw, _ := reader.ReadString('\n')
	routingKey := strings.TrimSpace(routingKeyRaw)

	fmt.Print("🧠 Enter headers JSON (e.g. {\"env\":\"prod\"}) or leave blank: ")
	headersRaw, _ := reader.ReadString('\n')
	headersJSON := strings.TrimSpace(headersRaw)

	fmt.Print("💣 Simulate failure? (yes/no): ")
	failRaw, _ := reader.ReadString('\n')
	failInput := strings.ToLower(strings.TrimSpace(failRaw))
	fail := "0"
	if failInput == "yes" || failInput == "y" {
		fail = "1"
	}

	// 🔁 Retry (Optional)
	fmt.Print("🔁 Retry this message? (yes/no): ")
	retryRaw, _ := reader.ReadString('\n')
	retry := "0"
	if strings.TrimSpace(strings.ToLower(retryRaw)) == "yes" {
		retry = "1"
	}

	fmt.Print("📡 Broadcast this message to all consumers? (yes/no): ")
	broadcastRaw, _ := reader.ReadString('\n')
	broadcast := strings.ToLower(strings.TrimSpace(broadcastRaw))

	// Prepare form data
	data := url.Values{}
	data.Set("task", task)
	data.Set("routingKey", routingKey)
	data.Set("headersJSON", headersJSON)
	data.Set("fail", fail)
	data.Set("retry", retry)
	data.Set("broadcast", broadcast)

	// Create HTTP request
	req, err := http.NewRequest("POST", "http://localhost:89/submit-task", bytes.NewBufferString(data.Encode()))
	if err != nil {
		fmt.Println("❌ Failed to create request:", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)

	// Send request
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("❌ Could not reach Load Balancer:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	fmt.Println("AA gya yahan")

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK {
		fmt.Println("✅ Task submitted successfully!")
		fmt.Println("📬 Feedback from worker:", string(body))
	} else {
		fmt.Printf("❌ Submission failed. Status: %s\n", resp.Status)
		fmt.Println("❗ Server response:", string(body))
	}
}
func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin": // macOS
		err = exec.Command("open", url).Start()
	default:
		fmt.Printf("⚠️ Please open this URL manually: %s\n", url)
	}
	if err != nil {
		fmt.Println("❌ Failed to open browser:", err)
	}
}
