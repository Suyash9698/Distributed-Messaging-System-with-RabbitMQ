package topic_exchange

import (
	"encoding/json"
	"log"
	"rabbitmq/monitor"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func StartTopicConsumer(bindingKey string) {
	err := Ch.ExchangeDeclare("topic_exchange_suyash", "topic", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("❌ Topic Exchange declare error: %v", err)
	}

	q, err := Ch.QueueDeclare(
		"",    // name (auto-generated)
		false, // durable
		true,  // auto-delete
		true,  // exclusive
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("❌ Topic Consumer: queue declare error: %v", err)
	}

	err = Ch.QueueBind(
		q.Name,
		bindingKey,
		"topic_exchange_suyash",
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("❌ Topic Consumer: queue bind error: %v", err)
	}

	msgs, err := Ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("❌ Topic Consumer: consume error: %v", err)
	}

	log.Printf("🔔 Topic Consumer: waiting for pattern: %s", bindingKey)

	go func() {
		for d := range msgs {
			startTs := time.Now() // ⏱️ Start time

			log.Printf("📥 [%s] %s", bindingKey, d.Body)

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

			err := d.Ack(false)
			if err != nil {
				log.Printf("❌ TopicConsumer Ack failed: %v", err)
			} else {
				monitor.MsgsAcked.Inc()
				monitor.WorkerThroughput.WithLabelValues(bindingKey).Inc()
				latMs := float64(time.Since(startTs).Milliseconds())
				monitor.TaskLatency.Observe(latMs)
			}
		}
	}()
}
