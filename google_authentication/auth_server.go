package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var (
	ctx = context.Background()
	rdb *redis.Client
)

var jwtSecret = []byte("super-secret-key-keep-this-safe")

var googleOauthConfig = &oauth2.Config{
	ClientID:     "<YOUR_CLIENT_ID>",
	ClientSecret: "<YOUR_CLIENT_SECRET>",
	RedirectURL:  "http://localhost:9000/oauth2/callback", // 👉 Dedicated auth server port
	Scopes: []string{
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	},
	Endpoint: google.Endpoint,
}

func main() {
	initRedis()
	http.HandleFunc("/generate-id", handleGenerateID)
	http.HandleFunc("/login", handleGoogleLogin)
	http.HandleFunc("/oauth2/callback", handleGoogleCallback)
	http.HandleFunc("/is-ready", handleIsReady)
	http.HandleFunc("/verify-token", handleVerifyToken) // ✅ register the endpoint
	http.HandleFunc("/fetch-token", handleFetchToken)

	log.Println("🔐 Auth server running on :9000")
	log.Fatal(http.ListenAndServe(":9000", nil))
}

func initRedis() {
	rdb = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("❌ Redis connection failed: %v", err)
	}
	log.Println("✅ Connected to Redis")
}

// 1️⃣ /generate-id → returns a unique user ID
func handleGenerateID(w http.ResponseWriter, r *http.Request) {
	// increment a key in redis, e.g. user_id_counter
	id, err := rdb.Incr(ctx, "user_id_counter").Result()
	if err != nil {
		http.Error(w, "Redis error", http.StatusInternalServerError)
		return
	}
	userId := strconv.FormatInt(id, 10)

	// set a small placeholder to mark userId is created
	rdb.Set(ctx, "login:status:"+userId, "created", 10*time.Minute)

	resp := map[string]string{"userId": userId}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// 2️⃣ /login?uid=xxx → user visits from main.go
func handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("uid")
	if uid == "" {
		http.Error(w, "Missing uid", http.StatusBadRequest)
		return
	}

	// store that user has started login
	rdb.Set(ctx, "login:status:"+uid, "started", 10*time.Minute)

	url := googleOauthConfig.AuthCodeURL("state-"+uid, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing code", http.StatusBadRequest)
		return
	}

	token, err := googleOauthConfig.Exchange(context.Background(), code)
	if err != nil {
		http.Error(w, "Token exchange failed", http.StatusInternalServerError)
		log.Println("Exchange error:", err)
		return
	}

	client := googleOauthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		http.Error(w, "User info fetch failed", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var user struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		http.Error(w, "JSON decode error", http.StatusInternalServerError)
		return
	}

	// ✅ Generate JWT
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"email": user.Email,
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	tokenString, err := jwtToken.SignedString(jwtSecret)
	if err != nil {
		http.Error(w, "JWT sign failed", http.StatusInternalServerError)
		return
	}

	html := fmt.Sprintf(`
	<!DOCTYPE html>
	<html lang="en">
	<head>
	  <meta charset="UTF-8">
	  <title>JWT Token</title>
	  <style>
		body {
		  background-color: #1e1e1e;
		  color: #d4d4d4;
		  font-family: "SF Mono", "Fira Code", monospace;
		  padding: 2rem;
		  display: flex;
		  justify-content: center;
		  align-items: center;
		  height: 100vh;
		}
		.container {
		  background: #2d2d2d;
		  border-radius: 10px;
		  padding: 2rem;
		  max-width: 800px;
		  width: 100%%;
		  box-shadow: 0 0 20px rgba(0,0,0,0.5);
		}
		.title {
		  font-size: 20px;
		  margin-bottom: 1rem;
		}
		.code-block {
		  background: #1e1e1e;
		  border: 1px solid #444;
		  border-radius: 8px;
		  padding: 1rem;
		  overflow-x: auto;
		  word-break: break-all;
		  white-space: pre-wrap;
		  position: relative;
		}
		button.copy {
		  position: absolute;
		  top: 10px;
		  right: 10px;
		  background-color: #3c3c3c;
		  color: #ddd;
		  border: 1px solid #555;
		  border-radius: 4px;
		  padding: 4px 10px;
		  cursor: pointer;
		  font-size: 12px;
		}
		button.copy:hover {
		  background-color: #555;
		}
		.copied {
		  color: #4caf50;
		  font-size: 14px;
		  margin-top: 10px;
		  display: none;
		}
	  </style>
	</head>
	<body>
	  <div class="container">
		<div class="title">✅ Your JWT Token</div>
		<div class="code-block" id="block">
		  <button class="copy" onclick="copyToClipboard()">Copy</button>
		  <code id="jwt">%s</code>
		</div>
		<div id="copiedText" class="copied">✅ Copied!</div>
	  </div>
	
	  <script>
		function copyToClipboard() {
		  const jwt = document.getElementById("jwt").innerText;
		  navigator.clipboard.writeText(jwt).then(() => {
			const copied = document.getElementById("copiedText");
			copied.style.display = "block";
			setTimeout(() => copied.style.display = "none", 2000);
		  });
		}
	  </script>
	</body>
	</html>
	`, tokenString)

	// store signal that user has completed login
	state := r.URL.Query().Get("state")
	if state == "" || len(state) <= 6 || state[:6] != "state-" {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}
	uid := state[6:] // Extract UID from "state-<uid>"

	rdb.Set(ctx, "login:token:"+uid, tokenString, 10*time.Minute)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))

}

// 4️⃣ /is-ready?uid=xxx → main.go polls to see if login done
func handleIsReady(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("uid")
	if uid == "" {
		http.Error(w, "Missing uid", http.StatusBadRequest)
		return
	}
	status, _ := rdb.Get(ctx, "login:status:"+uid).Result()
	if status == "done" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "done")
	} else {
		http.Error(w, "", http.StatusNotFound)
	}
}

func handleVerifyToken(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if auth == "" || len(auth) < 8 || auth[:7] != "Bearer " {
		http.Error(w, "Missing or invalid Authorization header", http.StatusBadRequest)
		return
	}
	tokenStr := auth[len("Bearer "):]

	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})

	if err != nil || !token.Valid {
		http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || claims["email"] == nil {
		http.Error(w, "Invalid token claims", http.StatusUnauthorized)
		return
	}

	email := claims["email"].(string)
	fmt.Fprintf(w, "✅ Token is valid!\nEmail: %s\n", email)
}
func handleFetchToken(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("userId")
	if uid == "" {
		http.Error(w, "Missing userId", http.StatusBadRequest)
		return
	}

	token, err := rdb.Get(ctx, "login:token:"+uid).Result()
	if err == redis.Nil {
		http.Error(w, "Token not ready", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Redis error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, token)
}
