package main

import (
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	conn, _ := amqp.Dial("amqp://guest:guest@localhost:5672/")
	defer conn.Close()
	ch, _ := conn.Channel()
	defer ch.Close()

	args := amqp.Table{
		"x-dead-letter-exchange":    "dlx_exchange",
		"x-dead-letter-routing-key": "dead_task",
	}

	_, err := ch.QueueDeclare(
		"resilient_queue",
		true,
		false,
		false,
		false,
		args,
	)
	if err != nil {
		log.Fatalf("❌ declare failed: %v", err)
	}

	msgs, err := ch.Consume("resilient_queue", "", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("❌ consume failed: %v", err)
	}

	log.Println("✅ Resilient consumer started")
	for d := range msgs {
		log.Printf("Got: %s", d.Body)
	}
}
