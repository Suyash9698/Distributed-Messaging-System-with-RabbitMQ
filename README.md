# Distributed Messaging System with RabbitMQ

> **A production-grade distributed messaging system built with Go, RabbitMQ, Redis, and Prometheus. Features include dynamic autoscaling, quorum queues, load-balanced gateway servers, Google OAuth2 authentication, advanced monitoring, and fault-tolerant node recovery.**

---

## ✨ Features

- **RabbitMQ Clustering** with quorum queues (Raft-based consensus)
- **Autoscaling Workers** based on queue depth
- **Load Balancing** across multiple gateway servers
- **Secure Authentication** using Google OAuth2
- **Header, Direct, Topic, Fanout Exchanges** fully utilized
- **Retry & Dead Letter Queues (DLQ)**
- **Redis-based Rate Limiting**
- **Prometheus + Grafana Monitoring**
- **Dockerized Setup** for easy deployment
- **Quorum Recovery Handler** for automatic node healing

---

## 🛠️ Tech Stack

- **Golang** (Concurrency, Channels)
- **RabbitMQ** (Messaging backbone)
- **Redis** (Token Cache, Rate Limiting)
- **Docker** (Containerization)
- **Prometheus** (Monitoring)
- **Grafana** (Visualization)

---

## 🖥️ System Architecture

```mermaid
flowchart TD

  subgraph Gateway Layer
    LB[Load Balancer]
    GW1[Gateway Server 1]
    GW2[Gateway Server 2]
  end

  subgraph RabbitMQ Cluster
    RMQ1[Quorum Node 1]
    RMQ2[Quorum Node 2]
    RMQ3[Quorum Node 3]
    DLX[Dead Letter Exchange]
    DLQ[Dead Letter Queue]
  end

  subgraph Worker Layer
    W1[Shard Worker 1]
    W2[Shard Worker 2]
    W3[Retry Worker]
    W4[Resilient Worker]
  end

  subgraph Monitoring Layer
    Prometheus
    Grafana
  end

  Client --> LB --> GW1
  Client --> LB --> GW2

  GW1 --> RMQ1
  GW2 --> RMQ2

  RMQ1 -->|Shard Queues| W1
  RMQ2 -->|Shard Queues| W2
  W1 -->|Retry to| W3
  W3 -->|Max Failures| DLQ
  DLX --> DLQ

  RMQ3 --> Prometheus
  W1 --> Prometheus
  W2 --> Prometheus
  W3 --> Prometheus

  Prometheus --> Grafana
```


---

## 🚀 Setup

```bash
# Clone the repository
$ git clone https://github.com/yourusername/distributed-messaging-system.git

# Spin up RabbitMQ cluster, Redis, Prometheus, Grafana
$ docker-compose up -d

# Run gateway servers
$ go run gateway/server.go

# Start worker pool
$ go run worker/main.go
```

---

## 📈 Monitoring Metrics

- `rabbitmq_queue_depth`
- `rabbitmq_dlq_depth`
- `rabbitmq_live_workers`
- `rabbitmq_messages_published`
- `rabbitmq_messages_acknowledged`
- `rabbitmq_task_latency_ms`

> Visualize all live metrics on a stunning custom Grafana dashboard!

---

## 🔒 Authentication Flow

- Google OAuth2 Authentication
- Redis caching for session tokens (expiry: 5 min)
- JWT tokens used for secured API access

---

## 🛡️ Fault Tolerance

- Quorum recovery when any RabbitMQ node fails
- Auto-scaling workers when queue load spikes
- Retry queues with exponential backoff for failed tasks

---

---

## 🙌 Acknowledgements

- RabbitMQ Official Documentation
- Prometheus and Grafana Community
- Go gRPC and Concurrency Best Practices
