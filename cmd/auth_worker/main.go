package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v4"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var rdb = redis.NewClient(&redis.Options{
	Addr: "localhost:6379",
})

var jwtSecret = []byte("super-secret-key-keep-this-safe")

var googleOauthConfig = &oauth2.Config{
	ClientID:     "<YOUR_CLIENT_ID>",
	ClientSecret: "<YOUR_CLIENT_SECRET>",
	RedirectURL:  "http://localhost:9000/oauth2/callback",
	Scopes: []string{
		"https://www.googleapis.com/auth/userinfo.email",
		"https://www.googleapis.com/auth/userinfo.profile",
	},
	Endpoint: google.Endpoint,
}

func main() {
	conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatal("Failed to connect to RabbitMQ:", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatal("Failed to open a channel:", err)
	}
	defer ch.Close()

	q, err := ch.QueueDeclare(
		"auth_queue", true, false, false, false, nil,
	)
	if err != nil {
		log.Fatal("Failed to declare queue:", err)
	}

	msgs, err := ch.Consume(q.Name, "auth_worker", true, false, false, false, nil)
	if err != nil {
		log.Fatal("Failed to register consumer:", err)
	}

	forever := make(chan bool)

	go func() {
		for d := range msgs {
			var payload struct {
				UserID string `json:"userId"`
			}
			if err := json.Unmarshal(d.Body, &payload); err != nil {
				log.Println("❌ Failed to decode payload:", err)
				continue
			}

			log.Println("🔄 Processing login for:", payload.UserID)
			issueToken(payload.UserID)
		}
	}()

	log.Println("🔄 Auth worker started and waiting for login requests...")
	<-forever
}

func issueToken(email string) {
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"email": email,
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	tokenString, err := jwtToken.SignedString(jwtSecret)
	if err != nil {
		log.Println("❌ Failed to sign JWT:", err)
		return
	}
	log.Printf("✅ Token for %s: %s\n", email, tokenString)

	// ✅ Store token in Redis
	ctx := context.Background()
	err = rdb.Set(ctx, email, tokenString, 5*time.Minute).Err()
	if err != nil {
		log.Println("❌ Failed to cache token:", err)
	}
}
