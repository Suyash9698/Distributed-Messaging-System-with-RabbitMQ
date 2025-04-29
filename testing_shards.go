// force_shard_test.go
package main

import (
	"bytes"
	"fmt"
	"net/http"
)

func main() {
	for i := 0; i < 20; i++ {
		payload := fmt.Sprintf("task=spam-%d", i)
		http.Post("http://localhost:89/submit-task",
			"application/x-www-form-urlencoded",
			bytes.NewBufferString(payload))
	}
}
