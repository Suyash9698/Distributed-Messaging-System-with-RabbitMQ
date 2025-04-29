package main

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func main() {
	gatewayURL := "http://localhost:89/submit-task" // Replace with your gateway's actual port

	for i := 0; i < 50; i++ {
		go func(i int) {
			data := url.Values{}
			data.Set("task", fmt.Sprintf("task-%d", i))
			resp, err := http.PostForm(gatewayURL, data)
			if err != nil {
				fmt.Println("❌ Error:", err)
			} else {
				fmt.Println("✅ Sent task", i, resp.Status)
				resp.Body.Close()
			}
		}(i)
		time.Sleep(5 * time.Millisecond) // Prevent overwhelming your HTTP server
	}

	time.Sleep(10 * time.Second) // Allow time for all requests to finish
}
