package monitor

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Quorum recovery metrics
var (
	QuorumRecoveriesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "quorum_recovery_total_recoveries",
		Help: "Total number of recovery attempts triggered for RabbitMQ quorum nodes",
	})

	QuorumRecoveriesFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "quorum_recovery_failures",
		Help: "Total number of failed recovery attempts",
	})

	QuorumRecoveryDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "quorum_recovery_success_duration_seconds",
		Help:    "Time taken to successfully recover a node (seconds)",
		Buckets: prometheus.ExponentialBuckets(1, 2, 6), // 1s to ~32s
	})
)

var (
	LiveWorkers = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rabbitmq_live_workers",
		Help: "Number of live workers",
	})

	QueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rabbitmq_queue_depth",
		Help: "Number of messages ready in task_queue",
	})

	DLQDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rabbitmq_dlq_depth",
		Help: "Number of messages in dead letter queue",
	})

	MsgsPublished = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rabbitmq_messages_published",
		Help: "Total messages published",
	})

	MsgsAcked = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rabbitmq_messages_acknowledged",
		Help: "Total messages acknowledged by workers",
	})

	WorkerThroughput = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rabbitmq_worker_throughput",
			Help: "Number of messages acknowledged per worker",
		},
		[]string{"worker_id"},
	)

	TaskLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "rabbitmq_task_latency_ms",
		Help:    "Time a worker spends processing one task",
		Buckets: prometheus.ExponentialBuckets(50, 2, 8),
	})

	QueueConsumers = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rabbitmq_queue_consumers",
		Help: "Number of consumers currently attached to task_queue",
	})

	DLXMessages = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rabbitmq_dlx_messages_total",
		Help: "Total number of messages sent to Dead Letter Exchange (DLX)",
	})

	DLQMessages = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rabbitmq_dlq_messages_total",
		Help: "Total number of messages dead-lettered into DLQ queue",
	})

	RetryMessages = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rabbitmq_retry_messages_total",
		Help: "Total number of retry attempts (sent to retry queue)",
	})

	FailureMessages = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "rabbitmq_failure_messages_total",
		Help: "Total number of failed messages (fallback path triggered)",
	})
)

func Init() {
	prometheus.MustRegister(
		MsgsAcked,
		MsgsPublished,
		WorkerThroughput,
		QueueDepth,
		DLQDepth,
		LiveWorkers,
		TaskLatency,
		QueueConsumers,
		DLXMessages,
		DLQMessages,
		RetryMessages,
		FailureMessages,

		// Register quorum recovery metrics
		QuorumRecoveriesTotal,
		QuorumRecoveriesFailed,
		QuorumRecoveryDuration,
	)
}
