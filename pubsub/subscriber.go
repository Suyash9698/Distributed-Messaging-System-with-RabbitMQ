package pubsub

import (
	"encoding/json"
	"log"
	"rabbitmq/monitor"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func StartFanoutConsumer() {
	err := Ch.ExchangeDeclare(
		"logs",
		"fanout",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("❌ PubSub Consumer: exchange declare error: %v", err)
	}

	q, err := Ch.QueueDeclare(
		"",    // random name
		false, // not durable
		true,  // auto-delete
		true,  // exclusive
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("❌ PubSub Consumer: queue declare error: %v", err)
	}

	err = Ch.QueueBind(
		q.Name,
		"",
		"logs",
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("❌ PubSub Consumer: queue bind error: %v", err)
	}

	msgs, err := Ch.Consume(
		q.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("❌ PubSub Consumer: consume error: %v", err)
	}

	log.Println("🔔 PubSub Consumer: Listening on 'logs' fanout...")

	go func() {
		for d := range msgs {
			startTs := time.Now() // ⏱️ Start timing

			if d.ReplyTo != "" {
				payload := struct {
					Status string `json:"status"`
				}{"success"}
				body, _ := json.Marshal(payload)
				Ch.Publish("", d.ReplyTo, false, false, amqp.Publishing{
					ContentType:   "application/json",
					Body:          body,
					CorrelationId: d.CorrelationId,
				})
			}

			log.Printf("📥 PubSub: Received: %s", d.Body)
			// 👇 Manual Ack + Metrics
			if err := d.Ack(false); err != nil {
				log.Printf("❌ DirectConsumer Ack failed: %v", err)
			} else {
				monitor.MsgsAcked.Inc()
				monitor.WorkerThroughput.WithLabelValues("direct").Inc()

				latMs := float64(time.Since(startTs).Milliseconds())
				monitor.TaskLatency.Observe(latMs)
			}
		}
	}()
}
