package headers_exchange

import (
	"log"
	"rabbitmq/monitor"
	"time"

	"encoding/json"

	amqp "github.com/rabbitmq/amqp091-go"
)

func StartHeadersConsumer() {
	err := Ch.ExchangeDeclare("headers_logs", "headers", true, false, false, false, nil)
	if err != nil {
		log.Fatalf("❌ Headers exchange declare error: %v", err)
	}

	q, err := Ch.QueueDeclare(
		"",
		false,
		true,
		true,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("❌ Headers queue declare error: %v", err)
	}

	bindingHeaders := amqp.Table{
		"x-match": "any",
		"env":     "prod",
	}
	err = Ch.QueueBind(
		q.Name,
		"",
		"headers_logs",
		false,
		bindingHeaders,
	)
	if err != nil {
		log.Fatalf("❌ Headers queue bind error: %v", err)
	}

	msgs, err := Ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("❌ Headers consume error: %v", err)
	}

	log.Printf("🔔 Waiting for messages with headers: %v", bindingHeaders)
	go func() {
		for d := range msgs {

			startTs := time.Now()

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

			log.Printf("📥 Received from headers exchange: %s", d.Body)
			// ✅ Acknowledge manually and increment metrics
			err := d.Ack(false)
			if err != nil {
				log.Printf("❌ HeadersConsumer Ack failed: %v", err)
			} else {
				monitor.MsgsAcked.Inc()
				monitor.WorkerThroughput.WithLabelValues("headers").Inc()

				latMs := float64(time.Since(startTs).Milliseconds())
				monitor.TaskLatency.Observe(latMs)
			}
		}
	}()
}
