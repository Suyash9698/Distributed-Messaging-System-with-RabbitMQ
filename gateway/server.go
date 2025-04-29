// gateway/server.go — Cleaned Auth Handling from Gateway
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt"
	amqp "github.com/rabbitmq/amqp091-go"

	"hash/fnv"
	dead_letter_queues "rabbitmq/dead_letter_queues"
	direct_exchange "rabbitmq/direct_exchange"
	headers_exchange "rabbitmq/headers_exchange"
	rate_limit "rabbitmq/limiter"
	monitor "rabbitmq/monitor"
	pubsub "rabbitmq/pubsub"
	"rabbitmq/quorum_recovery"
	topic_exchange "rabbitmq/topic_exchange"
	work_queue_model "rabbitmq/work_queue_model"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	conn           *amqp.Connection
	ch             *amqp.Channel
	rateLimiter    *rate_limit.RateLimiter
	publishedCount int64
)

// ── Consistent‑hash shard ring ─────────────────────────────────────────────────
var shardRing = []string{
	"task_shard_0",
	"task_shard_1",
	"task_shard_2",
	"task_shard_3",
}

func pickShard(key string) string {
	h := fnv.New32a()
	h.Write([]byte(key))
	return shardRing[int(h.Sum32())%len(shardRing)]
}

// ───────────────────────────────────────────────────────────────────────────────

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Default handler: path=%s method=%s", r.URL.Path, r.Method)
		http.NotFound(w, r)
	})

	http.HandleFunc("/submit-task", handleSubmitTask)
	log.Println("📍 All HTTP handlers registered")

	var err error
	rateLimiter = rate_limit.NewRateLimiter()

	conn, err = amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatalf("❌ Failed to connect to RabbitMQ: %v", err)
	}
	ch, err = conn.Channel()
	if err != nil {
		log.Fatalf("❌ Failed to open a channel: %v", err)
	}

	// ── Declare consistent‑hash shard queues ─────────────────────────
	// (we reuse your quorum‑queue args if desired)
	shardArgs := amqp.Table{
		"x-queue-type":              "quorum",
		"x-dead-letter-exchange":    "dlx_exchange",
		"x-dead-letter-routing-key": "dead_task",
	}
	for _, q := range shardRing {
		if _, err := ch.QueueDeclare(
			q,    // queue name
			true, // durable
			false,
			false,
			false,
			shardArgs,
		); err != nil {
			log.Fatalf("❌ failed to declare shard queue %s: %v", q, err)
		}
	}
	// ───────────────────────────────────────────────────────────────────

	monitor.Init()
	quorum_recovery.StartQuorumMonitor("http://localhost:15672", "guest", "guest")

	//
	/* ──────────────────────────────────────────────────────────────
	   RETURN‑HANDLER: catches any message the broker couldn’t route
	   and manually republishes it to the DLQ.
	   ──────────────────────────────────────────────────────────── */
	returns := ch.NotifyReturn(make(chan amqp.Return, 8))

	go func() {
		for ret := range returns {
			startTs := time.Now()
			log.Printf("↩️  Returned (unroutable) msg: %s –> send to DLQ", string(ret.Body))
			monitor.DLXMessages.Inc()

			// ① immediately tell the original client that the msg is dead‑lettered
			if ret.ReplyTo != "" {
				payload, _ := json.Marshal(struct {
					Status string `json:"status"`
				}{"dlq"})

				atomic.AddInt64(&publishedCount, 1)

				_ = ch.Publish(
					"", ret.ReplyTo, // default exchange routes by queue name
					false, false,
					amqp.Publishing{
						ContentType:   "application/json",
						Body:          payload,
						CorrelationId: ret.CorrelationId,
					},
				)
			}

			atomic.AddInt64(&publishedCount, 1)
			monitor.WorkerThroughput.WithLabelValues("dlx_handler").Inc()
			latMs := float64(time.Since(startTs).Milliseconds())
			monitor.TaskLatency.Observe(latMs)

			// ② forward the original message to the DLX, *keeping* meta‑data
			_ = ch.Publish(
				"dlx_exchange", "dead_task",
				false, false,
				amqp.Publishing{
					ContentType:   ret.ContentType,
					Body:          ret.Body,
					ReplyTo:       ret.ReplyTo,
					CorrelationId: ret.CorrelationId,
				},
			)
		}
	}()
	/* ────────────────────────────────────────────────────────────── */

	//mongo_api_store.InitMongoStore()

	direct_exchange.Ch = ch
	work_queue_model.Ch = ch
	topic_exchange.Ch = ch
	pubsub.Ch = ch
	headers_exchange.Ch = ch
	dead_letter_queues.Ch = ch

	dead_letter_queues.SetupRetryQueues(ch, shardRing)
	time.Sleep(100 * time.Millisecond)

	for i, q := range shardRing {
		go work_queue_model.StartTaskQueueWorker(q, fmt.Sprintf("shard-worker-%d", i))
		time.Sleep(100 * time.Millisecond)
	}

	// --- NEW: launch the auto‑scaler (every 5 s, if >50 msgs, add 2 workers)
	work_queue_model.StartAutoScaler(1*time.Second, 5, 2)

	for i, q := range shardRing {
		go dead_letter_queues.StartResilientWorker("resilient_"+q, fmt.Sprintf("shard-resilient-%d", i))
		time.Sleep(100 * time.Millisecond)
	}

	for i, q := range shardRing {
		go dead_letter_queues.StartDelayRetryWorker(ch, q+"_retry_5s", fmt.Sprintf("shard-delay-%d", i))
		time.Sleep(100 * time.Millisecond)
	}

	go direct_exchange.StartDirectConsumer("info")
	time.Sleep(100 * time.Millisecond)

	go topic_exchange.StartTopicConsumer("app.*")
	time.Sleep(100 * time.Millisecond)

	go pubsub.StartFanoutConsumer()
	time.Sleep(100 * time.Millisecond)

	go headers_exchange.StartHeadersConsumer()
	time.Sleep(100 * time.Millisecond)

	go dead_letter_queues.StartDLQConsumer()
	time.Sleep(100 * time.Millisecond)

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatalf("❌ Failed to bind to port: %v", err)
	}
	assignedPort := ln.Addr().(*net.TCPAddr).Port
	go registerWithLoadBalancer(assignedPort)

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("🚀 Gateway listening on :%d", assignedPort)
	http.HandleFunc("/metrics_manual", metricsHandler)
	http.Handle("/metrics", promhttp.Handler())
	if err := http.Serve(ln, nil); err != nil {
		log.Fatalf("❌ Server failed: %v", err)
	}

	defer conn.Close()
	defer ch.Close()
}

// metricsHandler serves Prometheus‑style metrics about our queue + workers.
func metricsHandler(w http.ResponseWriter, r *http.Request) {
	// 1) Inspect the main queue
	stats, err := ch.QueueInspect("task_queue")
	if err != nil {
		http.Error(w, fmt.Sprintf("QueueInspect error: %v", err), http.StatusInternalServerError)
		return
	}

	// 2) Inspect your dead‑letter queue (adjust name to your DLQ)
	dlqStats, err := ch.QueueInspect("dead_task")
	if err != nil {

		dlqStats.Messages = 0
	}

	queueDepth := stats.Messages
	dlqDepth := dlqStats.Messages
	liveWorkers := atomic.LoadInt32(&work_queue_model.WorkerCount)
	published := atomic.LoadInt64(&publishedCount)
	acknowledged := atomic.LoadInt64(&work_queue_model.AckCount)

	//manual_metric response
	out := fmt.Sprintf(`# HELP rabbitmq_queue_depth Number of messages ready in task_queue
        # TYPE rabbitmq_queue_depth gauge
            rabbitmq_queue_depth %d
        # HELP rabbitmq_dlq_depth Number of messages in dead letter queue
        # TYPE rabbitmq_dlq_depth gauge
            rabbitmq_dlq_depth %d
        # HELP rabbitmq_live_workers Number of live workers
        # TYPE rabbitmq_live_workers gauge
            rabbitmq_live_workers %d
        # HELP rabbitmq_messages_published Total messages published
        # TYPE rabbitmq_messages_published counter
            rabbitmq_messages_published %d
        # HELP rabbitmq_messages_acknowledged Total messages acknowledged by workers
        # TYPE rabbitmq_messages_acknowledged counter
            rabbitmq_messages_acknowledged %d
`, queueDepth, dlqDepth, liveWorkers, published, acknowledged)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.Write([]byte(out))
}

func handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST is allowed", http.StatusMethodNotAllowed)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || len(authHeader) < 8 {
		http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
		return
	}

	jwtToken := authHeader[len("Bearer "):len(authHeader)]
	token, err := jwt.Parse(jwtToken, func(t *jwt.Token) (interface{}, error) {
		return []byte("super-secret-key-keep-this-safe"), nil
	})

	if err != nil || !token.Valid {
		http.Error(w, "Invalid or expired JWT", http.StatusUnauthorized)
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || claims["email"] == nil {
		http.Error(w, "Invalid token claims", http.StatusUnauthorized)
		return
	}
	email := claims["email"].(string)
	log.Printf("🔐 Authenticated user: %s", email)

	// testing: 🔓 TEMPORARY BYPASS: skip JWT validation
	// email := "testuser@example.com"
	// log.Printf("🧪 [TEST MODE] Skipping JWT validation. Using fake email: %s", email)

	allowed, err := rateLimiter.Allow(email)
	if err != nil {
		log.Printf("⚠️ Rate limit error: %v", err)
		http.Error(w, "⚠️ Internal rate limit error", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "⚠️ 429 Too Many Requests", http.StatusTooManyRequests)
		return
	}

	// ─── 1) DECLARE A TEMP REPLY QUEUE ──────────────────────────────────
	replyQ, err := ch.QueueDeclare(
		"",    // server-generated name
		false, // non-durable
		true,  // auto-delete when connection closes
		true,  // exclusive
		false,
		nil,
	)
	if err != nil {
		http.Error(w, "Failed to declare reply queue", http.StatusInternalServerError)
		return
	}

	// ─── 2) START CONSUMING ON REPLY QUEUE ─────────────────────────────
	msgs, err := ch.Consume(
		replyQ.Name,
		"",   // consumer tag
		true, // auto-ack
		true, // exclusive
		false, false, nil,
	)
	if err != nil {
		http.Error(w, "Failed to consume reply queue", http.StatusInternalServerError)
		return
	}

	corrID := randomID()

	task := r.FormValue("task")
	if task == "" {
		http.Error(w, "Missing 'task' form field", http.StatusBadRequest)
		return
	}

	routingKey := r.FormValue("routingKey")
	headersJSON := r.FormValue("headersJSON")
	fail := r.FormValue("fail")
	retry := r.FormValue("retry")
	broadcast := r.FormValue("broadcast") == "yes"

	err = autoDetectAndPublish(ch, task, routingKey, headersJSON, fail, retry, replyQ.Name, corrID, !broadcast)
	if err != nil {
		http.Error(w, "Failed to publish: "+err.Error(), http.StatusInternalServerError)
		return
	}

	//─── 6) WAIT FOR FINAL WORKER FEEDBACK ─────────────────────────────
	type feedback struct {
		Status  string `json:"status"`
		Attempt int    `json:"attempt,omitempty"`
		Worker  string `json:"worker,omitempty"`
	}
	for d := range msgs {
		if d.CorrelationId != corrID {
			continue
		}
		var fb feedback
		_ = json.Unmarshal(d.Body, &fb)
		if fb.Status == "retry" {
			log.Printf("↻ retry #%d for %s", fb.Attempt, corrID)
			continue
		}
		// on "success" or "dlq", return JSON to client
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(d.Body)
		return
	}

	http.Error(w, "Timed out waiting for feedback", http.StatusGatewayTimeout)

	// testing: we don't wait for the worker reply here; fire‑and‑forget:
	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, `{"status":"accepted","task":"%s"}`, task)
	return
	//test ends here

	log.Printf("✔ Task submitted by: %s → %s", email, task)
	fmt.Fprintf(w, "✅ Task auto-routed: %s\n", task)
}

func autoDetectAndPublish(ch *amqp.Channel, task, routingKey, headersJSON, fail, retry, replyTo, corrID string, useShards bool) error {
	shard := pickShard(task)
	if retry == "1" {
		return func() error {
			fmt.Println("entering in retry loop")
			dead_letter_queues.SendToRetryQueue(ch, shard, task, replyTo, corrID)
			monitor.MsgsPublished.Inc()
			return nil
		}()
	}

	if fail == "1" {
		fmt.Println("entering in dlq failed")
		err := dead_letter_queues.PublishToResilientQueue(shard, task, replyTo, corrID)
		if err == nil {
			monitor.MsgsPublished.Inc()
		}
		return err
	}

	if headersJSON != "" {
		err := headers_exchange.PublishHeaders(task, headersJSON, replyTo, corrID)
		if err == nil {
			monitor.MsgsPublished.Inc()
		}
		return err
	}

	if strings.Contains(routingKey, "*") || strings.Contains(routingKey, "#") {
		err := topic_exchange.PublishTopic(task, routingKey, replyTo, corrID)
		if err == nil {
			monitor.MsgsPublished.Inc()
		}
		return err
	}

	if routingKey != "" {
		err := direct_exchange.PublishDirect(task, routingKey, replyTo, corrID)
		if err == nil {
			monitor.MsgsPublished.Inc()
		}
		return err
	}

	//monitor.MsgsPublished.Inc()
	//return pubsub.PublishFanout(task, replyTo, corrID)

	// ── Consistent‑hash fallback: publish into one of our quorum shards ───────
	if useShards {
		// 🧠 Consistent hashing
		shard := pickShard(task)
		err := ch.Publish(
			"", shard,
			false, false,
			amqp.Publishing{
				ContentType:   "text/plain",
				Body:          []byte(task),
				ReplyTo:       replyTo,
				CorrelationId: corrID,
			},
		)
		if err == nil {
			monitor.MsgsPublished.Inc()
		}
		return err
	} else {
		// 📡 Broadcast via fanout exchange
		return pubsub.PublishFanout(task, replyTo, corrID)
	}

	// ───────────────────────────────────────────────────────────────────────

	//testing: ─── fallback: put it straight on task_queue so the autoscaler sees it
	// log.Printf("📤 Fallback-publishing to task_queue: %q", task)
	// err := ch.Publish(
	// 	"",           // default exchange
	// 	"task_queue", // routing‑key == queue name
	// 	false, false, // mandatory, immediate
	// 	amqp.Publishing{
	// 		ContentType:   "text/plain",
	// 		Body:          []byte(task),
	// 		ReplyTo:       replyTo,
	// 		CorrelationId: corrID,
	// 	},
	// )
	// if err == nil {
	// 	monitor.MsgsPublished.Inc()
	// } else {
	// 	log.Printf("❌ Failed to publish to task_queue: %v", err)
	// }
	// return err
}

func registerWithLoadBalancer(port int) {
	lbURL := "http://localhost:89/register"
	hostname, _ := os.Hostname()
	self := fmt.Sprintf("http://localhost:%d", port)

	for retries := 0; retries < 5; retries++ {
		resp, err := http.PostForm(lbURL, map[string][]string{
			"address": {self},
			"name":    {hostname},
		})
		if err == nil && resp.StatusCode == 200 {
			log.Printf("✅ Registered with load balancer: %s", self)
			return
		}
		log.Printf("🔁 Retry LB registration: %v", err)
		time.Sleep(1 * time.Second)
	}
	log.Println("❌ Failed to register with load balancer")
}

func randomID() string {
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("%x", rand.Int63())
}
