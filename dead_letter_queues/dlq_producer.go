package dead_letter_queues

import (
	monitor "rabbitmq/monitor"

	amqp "github.com/rabbitmq/amqp091-go"
)

func PublishToResilientQueue(shard, body, replyTo, corrID string) error {
	resilientQ := "resilient_" + shard

	monitor.FailureMessages.Inc()

	return Ch.Publish(
		"",
		resilientQ,
		false,
		false,
		amqp.Publishing{
			DeliveryMode:  amqp.Persistent,
			ContentType:   "text/plain",
			Body:          []byte(body),
			ReplyTo:       replyTo,
			CorrelationId: corrID,
			Headers: amqp.Table{
				"x-force-fail": true,
			},
		},
	)
}
