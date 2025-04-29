package dead_letter_queues

import (
	"encoding/json"
	"log"
	"time"

	monitor "rabbitmq/monitor"

	amqp "github.com/rabbitmq/amqp091-go"
)

func StartDLQConsumer() {
	// Declare DLX
	err := Ch.ExchangeDeclare(
		"dlx_exchange",
		"direct",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("DLQ Consumer: exchange declare error: %v", err)
	}

	// Declare DLQ queue
	_, err = Ch.QueueDeclare(
		"dead_letter_queue",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("DLQ Consumer: queue declare error: %v", err)
	}

	// Bind DLQ to DLX
	err = Ch.QueueBind(
		"dead_letter_queue",
		"dead_task",
		"dlx_exchange",
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("DLQ Consumer: queue bind error: %v", err)
	}

	msgs, err := Ch.Consume(
		"dead_letter_queue",
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("DLQ Consumer: consume error: %v", err)
	}

	monitor.DLQMessages.Inc()

	log.Println("📦 DLQ Consumer started...")

	go func() {
		for d := range msgs {
			startTs := time.Now()

			if d.ReplyTo != "" {
				payload := struct {
					Status string `json:"status"`
				}{"dlq"}
				body, _ := json.Marshal(payload)
				Ch.Publish("", d.ReplyTo, false, false, amqp.Publishing{
					ContentType:   "application/json",
					Body:          body,
					CorrelationId: d.CorrelationId,
				})
			}

			log.Printf("💀 From DLQ: %s", d.Body)
			monitor.MsgsAcked.Inc()
			monitor.WorkerThroughput.WithLabelValues("dlq").Inc()
			latMs := float64(time.Since(startTs).Milliseconds())
			monitor.TaskLatency.Observe(latMs)
			d.Ack(false)
		}
	}()
}
