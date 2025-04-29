package work_queue_model

import (
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

func PublishToResilientQueue(task string) error {
	args := amqp.Table{
		"x-dead-letter-exchange":    "dlx_exchange",
		"x-dead-letter-routing-key": "dead_task",
		"x-queue-type":              "quorum",
	}
	_, err := Ch.QueueDeclare(
		"resilient_queue",
		true, false, false, false, args,
	)
	if err != nil {
		return err
	}

	err = Ch.Publish(
		"",
		"resilient_queue",
		false,
		false,
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        []byte(task),
		},
	)
	if err != nil {
		log.Printf("❌ Failed to publish to resilient queue: %v", err)
	}
	return err
}
