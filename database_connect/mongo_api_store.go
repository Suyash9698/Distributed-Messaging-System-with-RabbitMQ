package mongo_api_store

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type APIKey struct {
	Key       string    `bson:"key"`
	Owner     string    `bson:"owner"`
	Status    string    `bson:"status"`
	RateLimit int       `bson:"rateLimit"`
	CreatedAt time.Time `bson:"createdAt"`
	ExpiresAt time.Time `bson:"expiresAt,omitempty"`
}

var client *mongo.Client
var collection *mongo.Collection

// InitMongoStore connects to the MongoDB URI provided via .env or fallback
func InitMongoStore() {
	// Load .env file (if present)
	err := godotenv.Load()
	if err != nil {
		log.Println("⚠️ No .env file found. Falling back to hardcoded URI (if set).")
	}

	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		log.Fatal("❌ MONGO_URI not found in environment or .env file")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err = mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatalf("❌ MongoDB connection failed: %v", err)
	}

	db := client.Database("Mern")
	collection = db.Collection("api_keys")
	log.Println("✅ Connected to MongoDB API Key Store")
}

// ValidateAPIKey checks if the key exists and is active
func ValidateAPIKey(key string) (*APIKey, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var result APIKey
	err := collection.FindOne(ctx, bson.M{"key": key, "status": "active"}).Decode(&result)
	if err != nil {
		return nil, err
	}

	if !result.ExpiresAt.IsZero() && time.Now().After(result.ExpiresAt) {
		return nil, mongo.ErrNoDocuments
	}

	return &result, nil
}

func main() {
	InitMongoStore()

	key := "sk_test_51HxTtLCU9owPkwGc9KejqR5LTT7l7b9vQuqI2" // change this!
	record, err := ValidateAPIKey(key)
	if err != nil {
		fmt.Println("❌ Invalid or expired API key")
		return
	}

	fmt.Printf("✅ Authenticated %s owned by %s\n", key, record.Owner)
}
