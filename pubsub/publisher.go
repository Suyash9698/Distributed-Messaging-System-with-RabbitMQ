package pubsub

import (
	"log"
	"rabbitmq/monitor"

	amqp "github.com/rabbitmq/amqp091-go"
)

func PublishFanout(task, replyTo, corrID string) error {
	err := Ch.ExchangeDeclare(
		"logs",   // exchange name
		"fanout", // type
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	err = Ch.Publish(
		"logs",
		"",
		true,
		false,
		amqp.Publishing{
			ContentType:   "text/plain",
			Body:          []byte(task),
			ReplyTo:       replyTo,
			CorrelationId: corrID,
		})
	if err != nil {
		return err
	}

	log.Printf("📤 PubSub: Published to fanout 'logs': %s", task)
	monitor.MsgsPublished.Inc()
	return nil
}
