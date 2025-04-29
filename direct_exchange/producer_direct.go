package direct_exchange

import (
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

func PublishDirect(task, routingKey, replyTo, corrID string) error {

	err := Ch.ExchangeDeclare("direct_logs", "direct", true, false, false, false, nil)
	if err != nil {
		return err
	}

	err = Ch.Publish(
		"direct_logs",
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

	log.Printf("📤 Direct Exchange: Sent [%s]: %s", routingKey, task)
	return nil
}
