package dead_letter_queues

import (
	"encoding/json"
	"log"
	"math/rand"
	monitor "rabbitmq/monitor"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func StartResilientWorker(queueName, workerID string) {
	rand.Seed(time.Now().UnixNano())

	args := amqp.Table{
		"x-queue-type":              "quorum",
		"x-dead-letter-exchange":    "dlx_exchange",
		"x-dead-letter-routing-key": "dead_task",
	}

	_, err := Ch.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		args,
	)
	if err != nil {
		log.Fatalf("Worker: queue declare error: %v", err)
	}

	msgs, err := Ch.Consume(
		queueName,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Worker: consume error: %v", err)
	}

	log.Println("🛠️ Resilient worker started...")

	go func() {
		for d := range msgs {
			startTs := time.Now()
			log.Printf("📥 Received: %s", d.Body)

			forceFail, _ := d.Headers["x-force-fail"].(bool)
			shouldFail := forceFail || rand.Float32() < 0.5

			if shouldFail {
				log.Println("❌ Failure – routing to DLQ")

				// ---- send 'dlq' feedback to the caller ----------
				if d.ReplyTo != "" {
					fb, _ := json.Marshal(struct {
						Status string `json:"status"`
						Worker string `json:"worker"`
						Reason string `json:"reason"`
					}{
						Status: "dlq",
						Worker: workerID,
						Reason: "forced failure",
					})
					_ = Ch.Publish(
						"",
						d.ReplyTo,
						false,
						false,
						amqp.Publishing{
							ContentType:   "application/json",
							Body:          fb,
							CorrelationId: d.CorrelationId,
						},
					)
				}

				// Nack ⇒ message hits dlx_exchange
				d.Nack(false, false)
				continue
			}

			/* -------------------- SUCCESS path --------------------------- */
			if d.ReplyTo != "" {
				fb, _ := json.Marshal(struct {
					Status string `json:"status"`
					Worker string `json:"worker"`
				}{"success", workerID})
				_ = Ch.Publish(
					"",
					d.ReplyTo,
					false,
					false,
					amqp.Publishing{
						ContentType:   "application/json",
						Body:          fb,
						CorrelationId: d.CorrelationId,
					},
				)
			}

			log.Println("✅ Processed successfully.")

			monitor.MsgsAcked.Inc()
			monitor.WorkerThroughput.WithLabelValues(workerID).Inc()

			latMs := float64(time.Since(startTs).Milliseconds())
			monitor.TaskLatency.Observe(latMs)

			d.Ack(false)
		}
	}()
}
