package dead_letter_queues

import (
	"log"

	monitor "rabbitmq/monitor"

	amqp "github.com/rabbitmq/amqp091-go"
)

func SendToRetryQueue(ch *amqp.Channel, shard, msg, replyTo, corrID string) error {
	retryQ := shard + "_retry_5s"
	err := ch.Publish(
		"retry_exchange", retryQ, false, false,
		amqp.Publishing{
			Headers:       amqp.Table{"x-retry-count": int32(0)},
			DeliveryMode:  amqp.Persistent,
			ContentType:   "text/plain",
			Body:          []byte(msg),
			ReplyTo:       replyTo,
			CorrelationId: corrID,
		},
	)
	if err != nil {
		log.Printf("❌ Failed to publish to main_queue: %v", err)
		return err
	}
	monitor.RetryMessages.Inc()
	log.Printf("📤 Sent to main_queue with ReplyTo %s", replyTo)
	return nil
}
