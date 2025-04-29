package dead_letter_queues

import (
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

// SetupRetryQueues declares per-shard retry delay queues and binds them to a direct retry exchange.
func SetupRetryQueues(ch *amqp.Channel, shardRing []string) {
	// 1) Declare a direct exchange for retries
	err := ch.ExchangeDeclare(
		"retry_exchange", // name of the exchange
		"direct",         // exchange type
		true,             // durable
		false,            // auto-deleted when unused
		false,            // internal use only
		false,            // no-wait
		nil,              // arguments
	)
	if err != nil {
		log.Fatalf("❌ Failed to declare retry_exchange: %v", err)
	}

	// 2) For each shard, declare a delay queue with TTL and dead-letter back to the shard
	for _, shard := range shardRing {
		deltaQueue := shard + "_retry_5s"
		args := amqp.Table{
			"x-message-ttl":             int32(5000), // 5 seconds
			"x-dead-letter-exchange":    "",          // default exchange
			"x-dead-letter-routing-key": shard,       // route back to original shard
		}

		// Declare per-shard delay queue
		if _, err := ch.QueueDeclare(
			deltaQueue,
			true,  // durable
			false, // auto-delete when unused
			false, // exclusive
			false, // no-wait
			args,
		); err != nil {
			log.Fatalf("❌ Failed to declare delay queue %s: %v", deltaQueue, err)
		}

		// Bind the delay queue to the retry exchange using the same name as routing key
		if err := ch.QueueBind(
			deltaQueue,       // queue name
			deltaQueue,       // routing key
			"retry_exchange", // exchange name
			false,
			nil,
		); err != nil {
			log.Fatalf("❌ Failed to bind delay queue %s: %v", deltaQueue, err)
		}
	}

	log.Println("✅ Per-shard retry queues and bindings set up")
}
