package topic_exchange

import (
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

func PublishTopic(task, routingKey, replyTo, corrID string) error {
	err := Ch.ExchangeDeclare("topic_exchange_suyash", "topic", true, false, false, false, nil)
	if err != nil {
		return err
	}

	err = Ch.Publish(
		"topic_exchange_suyash",
		routingKey,
		true,
		false,
		amqp.Publishing{
			ContentType:   "text/plain",
			Body:          []byte(task),
			ReplyTo:       replyTo,
			CorrelationId: corrID,
		},
	)
	if err != nil {
		return err
	}

	log.Printf("📤 Topic Exchange: Sent [%s]: %s", routingKey, task)
	return nil
}
