package dead_letter_queues

import (
	"encoding/json"
	"log"
	"math/rand"
	monitor "rabbitmq/monitor"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func StartDelayRetryWorker(ch *amqp.Channel, retryQueue, workerID string) {
	rand.Seed(time.Now().UnixNano())

	msgs, err := ch.Consume(
		retryQueue, // now using the passed-in retryQueue name
		"",
		false, // manual ack
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("❌ Failed to consume %q: %v", retryQueue, err)
	}

	log.Printf("⏳ Delay retry worker started on %q...", retryQueue)

	go func() {
		for d := range msgs {
			startTs := time.Now()
			msg := string(d.Body)
			var retryCount int32
			if hdr, ok := d.Headers["x-retry-count"]; ok {
				switch v := hdr.(type) {
				case int32:
					retryCount = v
				case int64:
					retryCount = int32(v)
				case float64:
					retryCount = int32(v)
				}
			}

			log.Printf("📥 [%s] got retry #%d: %q", retryQueue, retryCount, msg)

			// always "fail" for retry logic
			if retryCount < 3 {

				retryCount++
				log.Printf("🔁 [%s] re-queueing attempt #%d", retryQueue, retryCount)

				// send interim retry status back to caller
				if d.ReplyTo != "" {
					payload := struct {
						Status  string `json:"status"`
						Attempt int32  `json:"attempt"`
						Worker  string `json:"worker"`
					}{"retry", retryCount, workerID}
					body, _ := json.Marshal(payload)
					_ = ch.Publish("", d.ReplyTo, false, false, amqp.Publishing{
						ContentType:   "application/json",
						Body:          body,
						CorrelationId: d.CorrelationId,
					})
				}

				// republish into the same retry queue via retry_exchange
				err := ch.Publish(
					"retry_exchange", // direct exchange with per‑shard bindings
					retryQueue,       // routing key == queue name
					false, false,
					amqp.Publishing{
						ContentType:   "text/plain",
						Headers:       amqp.Table{"x-retry-count": retryCount},
						DeliveryMode:  amqp.Persistent,
						Body:          d.Body,
						ReplyTo:       d.ReplyTo,
						CorrelationId: d.CorrelationId,
					},
				)
				if err != nil {
					log.Printf("❌ [%s] Failed to republish: %v", retryQueue, err)
					d.Nack(false, false)

				} else {
					d.Ack(false)
					monitor.MsgsAcked.Inc()
					monitor.WorkerThroughput.WithLabelValues(workerID).Inc()

					latMs := float64(time.Since(startTs).Milliseconds())
					monitor.TaskLatency.Observe(latMs)
				}
			} else {
				log.Printf("💀 [%s] Max retries reached, sending to DLQ", retryQueue)
				if d.ReplyTo != "" {
					payload := struct {
						Status string `json:"status"`
						Worker string `json:"worker"`
					}{"dlq", workerID}
					body, _ := json.Marshal(payload)
					_ = ch.Publish("", d.ReplyTo, false, false, amqp.Publishing{
						ContentType:   "application/json",
						Body:          body,
						CorrelationId: d.CorrelationId,
					})
				}
				d.Ack(false)
				monitor.MsgsAcked.Inc()
				monitor.WorkerThroughput.WithLabelValues(workerID).Inc()

				latMs := float64(time.Since(startTs).Milliseconds())
				monitor.TaskLatency.Observe(latMs)
			}
		}
	}()
}
