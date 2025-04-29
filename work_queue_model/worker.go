package work_queue_model

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"

	monitor "rabbitmq/monitor"

	amqp "github.com/rabbitmq/amqp091-go"
)

var WorkerCount int32

var AckCount int64

// ── Define the same shard ring here ────────────────────────────────
var shardRing = []string{
	"task_shard_0",
	"task_shard_1",
	"task_shard_2",
	"task_shard_3",
}

// ───────────────────────────────────────────────────────────────────

func StartTaskQueueWorker(queueName, id string) {

	newCount := atomic.AddInt32(&WorkerCount, 1)
	monitor.LiveWorkers.Set(float64(newCount))

	// 2) ensure the queue exists
	args := amqp.Table{
		"x-queue-type":              "quorum",
		"x-dead-letter-exchange":    "dlx_exchange",
		"x-dead-letter-routing-key": "dead_task",
	}
	_, err := Ch.QueueDeclare(queueName, true, false, false, false, args)
	if err != nil {
		log.Fatalf("❌ TaskQueueWorker: QueueDeclare failed: %v", err)
	}

	// 3) only prefetch one at a time
	if err := Ch.Qos(1, 0, false); err != nil {
		log.Fatalf("❌ TaskQueueWorker: Qos failed: %v", err)
	}

	// 4) start consuming
	msgs, err := Ch.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		log.Fatalf("❌ TaskQueueWorker: Consume failed: %v", err)
	}

	log.Printf("👷 [%s] Task worker started…", id)

	// 5) process messages in the background
	go func() {
		for d := range msgs {
			startTs := time.Now()

			log.Printf("📥 [%s] Received: %s", id, d.Body)
			// simulate work
			time.Sleep(100 * time.Millisecond)
			log.Printf("✅ [%s] Done", id)

			// 5a) optional reply‐to
			if d.ReplyTo != "" {
				resp := struct {
					Status string `json:"status"`
					Worker string `json:"worker"`
				}{"success", id}
				body, _ := json.Marshal(resp)

				if err := Ch.Publish(
					"", d.ReplyTo,
					false, false,
					amqp.Publishing{
						ContentType:   "application/json",
						Body:          body,
						CorrelationId: d.CorrelationId,
					},
				); err != nil {
					log.Printf("❌ [%s] Failed to publish reply: %v", id, err)
				} else {
					// count replies as “published”
					monitor.MsgsPublished.Inc()
				}
			}

			// 5b) ack and record metrics
			if err := d.Ack(false); err != nil {
				log.Printf("❌ [%s] Ack failed: %v", id, err)
			} else {
				monitor.MsgsAcked.Inc()
				monitor.WorkerThroughput.WithLabelValues(id).Inc()

				latMs := float64(time.Since(startTs).Milliseconds())
				monitor.TaskLatency.Observe(latMs)

				atomic.AddInt64(&AckCount, 1)
			}
		}

		// 6) when msgs channel closes, this worker is “dead” — decrement
		newCount := atomic.AddInt32(&WorkerCount, -1)
		monitor.LiveWorkers.Set(float64(newCount))
	}()
}

/*───────────────────────────────────────────*
|           ‼️  Auto‑Scaler            |
*───────────────────────────────────────────*/

// if the depth > threshold, spawns add new workers.
func StartAutoScaler(interval time.Duration, threshold, burst int) {

	logFile, err := os.OpenFile("autoscaler.log",
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatalf("autoscaler: could not open log file: %v", err)
	}

	logger := log.New(logFile, "autoscaler: ", log.LstdFlags)
	logger.Println("🧵 Auto‑scaler initialized")
	go func() {
		for {
			time.Sleep(interval)

			// ➊ inspect each shard’s depth
			for _, queueName := range shardRing {
				stats, err := Ch.QueueInspect(queueName)
				if err != nil {
					logger.Printf("⚠️ Auto‑scaler: inspect failed on %s: %v", queueName, err)
					continue
				}
				depth := stats.Messages

				logger.Printf("ℹ️ [%s] depth=%d liveWorkers=%d",
					queueName, depth, atomic.LoadInt32(&WorkerCount))

				// ➋ spin up more if needed
				if depth > threshold {
					for i := 0; i < burst; i++ {
						id := fmt.Sprintf("%s-auto-%d", queueName, time.Now().UnixNano())
						StartTaskQueueWorker(queueName, id)
					}
					logger.Printf("🔁 Spinning up %d more workers for %s (depth=%d)",
						burst, queueName, depth)
				}
			}
		}
	}()
}

// StartResilientConsumer
func StartResilientConsumer() {
	args := amqp.Table{
		"x-queue-type":              "quorum", // 💡 quorum enabled
		"x-dead-letter-exchange":    "dlx_exchange",
		"x-dead-letter-routing-key": "dead_task",
	}

	_, err := Ch.QueueDeclare(
		"resilient_queue",
		true,
		false,
		false,
		false,
		args,
	)
	if err != nil {
		log.Fatalf("❌ ResilientConsumer: queue declare failed: %v", err)
	}

	msgs, err := Ch.Consume(
		"resilient_queue",
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("❌ ResilientConsumer: consume failed: %v", err)
	}

	log.Println("🧑‍🔧 Resilient consumer started")
	go func() {
		for d := range msgs {
			log.Printf("📥 Resilient Worker got task: %s", d.Body)
		}
	}()
}
