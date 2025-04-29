package google_authentication

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var JwtSecret = []byte("super-secret-key-keep-this-safe")

var googleOauthConfig = &oauth2.Config{
	ClientID:     "<YOUR_CLIENT_ID>",
	ClientSecret: "<YOUR_CLIENT_SECRET>",
	RedirectURL:  "http://localhost:89/oauth2/callback", // 👈 Load balancer port
	Scopes: []string{
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	},
	Endpoint: google.Endpoint,
}

// /login → redirects user to Google
// /login → redirects user to Google
func HandleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	// Adding oauth2.SetAuthURLParam("prompt", "select_account") forces the account chooser
	url := googleOauthConfig.AuthCodeURL("random-state-string", oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "select_account"))
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// /oauth2/callback → exchange code and return JWT
func HandleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing code", http.StatusBadRequest)
		return
	}

	token, err := googleOauthConfig.Exchange(context.Background(), code)
	if err != nil {
		http.Error(w, "Failed to exchange code", http.StatusInternalServerError)
		log.Println("Exchange error:", err)
		return
	}

	client := googleOauthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var user struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		http.Error(w, "Failed to decode user info", http.StatusInternalServerError)
		return
	}

	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"email": user.Email,
		"exp":   time.Now().Add(30 * time.Minute).Unix(),
	})

	tokenString, err := jwtToken.SignedString(JwtSecret)
	if err != nil {
		http.Error(w, "Failed to sign JWT", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "✅ Logged in!\nYour JWT token:\n\n%s\n", tokenString)
}
