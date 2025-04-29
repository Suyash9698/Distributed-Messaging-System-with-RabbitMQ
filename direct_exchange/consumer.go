package direct_exchange

import (
	"encoding/json"
	"log"
	"rabbitmq/monitor"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func StartDirectConsumer(routingKey string) {
	err := Ch.ExchangeDeclare("direct_logs", "direct", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("❌ Consumer exchange declare error: %v", err)
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
		log.Fatalf("❌ Queue declare error: %v", err)
	}

	err = Ch.QueueBind(
		q.Name,
		routingKey,
		"direct_logs",
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("❌ Queue bind error: %v", err)
	}

	msgs, err := Ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("❌ Consume error: %v", err)
	}

	log.Printf("🔔 Listening on direct_logs for routing key: %s", routingKey)

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

			log.Printf("📥 [%s] %s", routingKey, d.Body)
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
