package limiter

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

type RateLimiter struct {
	Client      *redis.Client
	MaxRequests int
	Window      time.Duration
}

func NewRateLimiter() *RateLimiter {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	return &RateLimiter{
		Client:      rdb,
		MaxRequests: 10,
		Window:      time.Minute,
	}
}

func (r *RateLimiter) Allow(apiKey string) (bool, error) {
	key := fmt.Sprintf("rate_limit:%s", apiKey)

	pipe := r.Client.TxPipeline()

	count := pipe.Incr(ctx, key)

	pipe.Expire(ctx, key, r.Window)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}

	// checking if rate limit exceeded
	if countVal := count.Val(); countVal > int64(r.MaxRequests) {
		return false, nil
	}
	return true, nil
}
