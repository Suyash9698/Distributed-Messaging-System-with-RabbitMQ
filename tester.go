package quorum_recovery

//older version
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

type Queue struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Leader  string   `json:"leader"`
	Members []string `json:"members"`
}

var forgotten = make(map[string]bool)

var expectedNodes = []string{
	"rabbit@rabbit1",
	"rabbit@rabbit2",
	"rabbit@rabbit3",
}

func StartQuorumMonitor(rabbitAPIURL, user, pass string) {
	go func() {
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			log.Fatalf("cannot connect to docker daemon: %v", err)
		}
		defer cli.Close()

		for {
			checkQuorumStatus(context.Background(), cli, rabbitAPIURL, user, pass)
			time.Sleep(10 * time.Second)
		}
	}()
}

func checkQuorumStatus(ctx context.Context, cli *client.Client, url, user, pass string) {
	log.Println("🔍 [quorum_recovery] polling cluster status…")

	// 1) nodes
	resp, err := httpRequest(url+"/api/nodes", user, pass)
	if err != nil {
		log.Println("❌ fetch nodes:", err)
		return
	}
	var raw []struct {
		Name    string `json:"name"`
		Running bool   `json:"running"`
	}
	if err := json.Unmarshal(resp, &raw); err != nil {
		log.Println("❌ decode nodes:", err)
		return
	}
	live := make(map[string]bool)
	for _, n := range raw {
		live[n.Name] = n.Running
		if !n.Running {
			log.Printf("⚠️ Node %s known but not running", n.Name)
			triggerRecovery(ctx, cli, n.Name)
		}
	}
	for _, name := range expectedNodes {
		if !live[name] {
			log.Printf("⚠️ Node %s missing from /api/nodes", name)
			triggerRecovery(ctx, cli, name)
		}
	}

	// 2) queues
	resp, err = httpRequest(url+"/api/queues", user, pass)
	if err != nil {
		log.Println("❌ fetch queues:", err)
		return
	}
	var queues []Queue
	if err := json.Unmarshal(resp, &queues); err != nil {
		log.Println("❌ decode queues:", err)
		return
	}
	for _, q := range queues {
		if q.Type != "quorum" {
			continue
		}
		if len(q.Members) == 0 {
			log.Printf("⚠️ Quorum on %s: %d members", q.Name, len(q.Members))
			// only recover the leader if it’s actually down
			if !live[q.Leader] {
				triggerRecovery(ctx, cli, q.Leader)
			}
		}
		for _, m := range q.Members {
			if !live[m] {
				log.Printf("⚠️ Dead node %s for queue %s", m, q.Name)
				triggerRecovery(ctx, cli, m)
			}
		}
	}
}

func httpRequest(url, user, pass string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(user, pass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func extractContainer(node string) string {
	switch node {
	case "rabbit@rabbit1":
		return "rabbit1"
	case "rabbit@rabbit2":
		return "rabbit2"
	case "rabbit@rabbit3":
		return "rabbit3"
	}
	return ""
}

func triggerRecovery(ctx context.Context, cli *client.Client, deadNode string) {
	if deadNode == "" || forgotten[deadNode] {
		return
	}
	forgotten[deadNode] = true

	container := extractContainer(deadNode)
	if container == "" {
		log.Printf("❌ no container for %s", deadNode)
		return
	}

	// 1) stop
	log.Printf("🛑 Stopping container %s…", container)
	timeout := 10 * time.Second
	if err := cli.ContainerStop(ctx, container, &timeout); err != nil {
		log.Printf("❌ stop %s: %v", container, err)
	}

	// 2) forget with exponential back‑off
	maxRetries := 5
	backoff := 1 * time.Second
	for i := 1; i <= maxRetries; i++ {
		log.Printf("💀 offline‑forget %s (try #%d)…", deadNode, i)
		out, err := execInContainer(ctx, cli, "rabbit1",
			"rabbitmqctl", "forget_cluster_node", deadNode, "--offline",
		)
		if err == nil {
			log.Printf("✅ forgotten %s:\n%s", deadNode, out)
			break
		}
		if i == maxRetries {
			log.Printf("❌ could not forget %s after %d tries: %v\n%s",
				deadNode, maxRetries, err, out)
			return
		}
		sleep := backoff + time.Duration(rand.Intn(500))*time.Millisecond
		log.Printf("⏳ retrying in %v…", sleep)
		time.Sleep(sleep)
		backoff *= 2
	}

	// 3) reset & start
	info, err := cli.ContainerInspect(ctx, container)
	if err != nil {
		log.Printf("❌ inspect %s: %v", container, err)
		return
	}
	if !info.State.Running {
		log.Printf("🔁 Starting container %s...", container)
		if err := cli.ContainerStart(ctx, container, types.ContainerStartOptions{}); err != nil {
			log.Printf("❌ start %s: %v", container, err)
			return
		}
		time.Sleep(5 * time.Second)
	}

	log.Printf("🔄 Resetting Mnesia in %s…", container)
	if _, err := execInContainer(ctx, cli, container, "rabbitmqctl", "reset"); err != nil {
		log.Printf("❌ reset %s: %v", container, err)
	}

	log.Printf("▶️ Starting app in %s…", container)
	if _, err := execInContainer(ctx, cli, container, "rabbitmqctl", "start_app"); err != nil {
		log.Printf("❌ start_app %s: %v", container, err)
	}
}

func execInContainer(ctx context.Context, cli *client.Client, container string, cmd ...string) ([]byte, error) {
	cfg := types.ExecConfig{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	}
	cre, err := cli.ContainerExecCreate(ctx, container, cfg)
	if err != nil {
		return nil, err
	}
	att, err := cli.ContainerExecAttach(ctx, cre.ID, types.ExecStartCheck{})
	if err != nil {
		return nil, err
	}
	defer att.Close()
	out, _ := io.ReadAll(att.Reader)

	ins, err := cli.ContainerExecInspect(ctx, cre.ID)
	if err != nil {
		return out, err
	}
	if ins.ExitCode != 0 {
		return out, fmt.Errorf("exit code %d", ins.ExitCode)
	}
	return out, nil
}
