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
	"path/filepath"
	"rabbitmq/monitor"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

type Queue struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Leader  string   `json:"leader"`
	Members []string `json:"members"`
}

var forgotten = make(map[string]bool)

var expectedNodes []string
var previousExpectedNodes []string

func updateExpectedNodes(ctx context.Context, cli *client.Client) {
	previousExpectedNodes = append([]string(nil), expectedNodes...)
	expectedNodes = []string{} // clear the old list

	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{All: false})
	if err != nil {
		log.Printf("❌ Could not list containers: %v", err)
		return
	}

	for _, c := range containers {
		if strings.HasPrefix(c.Names[0], "/rabbit") {
			// container name is like "/rabbit1" or "/rabbit-recover-590"
			name := strings.TrimPrefix(c.Names[0], "/")
			node := fmt.Sprintf("rabbit@%s", name)
			expectedNodes = append(expectedNodes, node)
		}
	}
	log.Printf("🔁 Updated expectedNodes: %v", expectedNodes)
}

func addTestNode(ctx context.Context, cli *client.Client, containerName, nodeName, cookie, user, pass string) error {
	log.Printf("🚀 Creating container %s (%s)…", containerName, nodeName)

	cfg := &container.Config{
		Image:    "rabbitmq:3.12-management",
		Hostname: containerName, // dynamic hostname
		Env: []string{
			"RABBITMQ_NODENAME=" + nodeName,
			"RABBITMQ_DEFAULT_USER=" + user,
			"RABBITMQ_DEFAULT_PASS=" + pass,
			"RABBITMQ_ERLANG_COOKIE=" + cookie,
		},
	}

	confPath, _ := filepath.Abs("./config/rabbitmq.conf")
	pluginPath, _ := filepath.Abs("./config/enabled_plugins")
	hostCfg := &container.HostConfig{
		NetworkMode: "rabbitmq_rabbitmq_cluster",
		Binds: []string{
			fmt.Sprintf("%s-data:/var/lib/rabbitmq", containerName), // dynamic volume
			confPath + ":/etc/rabbitmq/rabbitmq.conf:ro",
			pluginPath + ":/etc/rabbitmq/enabled_plugins:ro",
		},
	}

	netCfg := &network.NetworkingConfig{}

	resp, err := cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, containerName)
	if err != nil {
		log.Printf("❌ failed to create %s: %v", containerName, err)
		return err
	}
	log.Printf("📦 %s container created", containerName)

	if err := cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		log.Printf("❌ failed to start %s: %v", containerName, err)
		return err
	}
	log.Printf("⚙️ %s started, waiting for initialization…", containerName)
	time.Sleep(10 * time.Second)

	// Wait for RabbitMQ to be ready before calling stop_app
	log.Printf("⚙️ %s started, waiting for RabbitMQ to be ready…", containerName)

	maxWait := 90 * time.Second
	interval := 3 * time.Second
	deadline := time.Now().Add(maxWait)

	ready := false
	for {
		if time.Now().After(deadline) {
			log.Printf("❌ timeout: RabbitMQ not ready in %v", maxWait)
			return fmt.Errorf("rabbitmq not ready")
		}

		out, err := execInContainer(ctx, cli, containerName, "rabbitmqctl", "status")
		if err == nil && string(out) != "" {
			log.Printf("✅ RabbitMQ ready on %s", containerName)
			ready = true
			break
		}
		log.Printf("⏳ waiting for %s to be ready...", containerName)
		time.Sleep(interval)
	}

	if !ready {
		return fmt.Errorf("RabbitMQ never became ready")
	}

	// 1) Stop the RabbitMQ application inside the new container
	log.Printf("⛔ Stopping RabbitMQ on %s before join…", containerName)
	out, err := execInContainer(ctx, cli, containerName,
		"rabbitmqctl", "-n", nodeName, "stop_app",
	)
	if err != nil {
		log.Printf("❌ stop_app failed on %s: %v — %s", containerName, err, out)
		return err
	}
	log.Printf("✅ stop_app on %s:\n%s", containerName, out)

	// 2) Join the cluster as a disc node
	if out, err := execInContainer(ctx, cli, containerName,
		"rabbitmqctl", "join_cluster", "rabbit@rabbit1"); err != nil {
		log.Printf("❌ join_cluster failed: %v — %s", err, out)
		return err
	} else {
		log.Printf("✅ join_cluster success: %s", out)
	}

	log.Printf("▶️ Starting %s app after join…", containerName)
	if out, err := execInContainer(ctx, cli, containerName, "rabbitmqctl", "start_app"); err != nil {
		log.Printf("❌ start_app failed: %v — %s", err, out)
		return err
	} else {
		log.Printf("✅ start_app success: %s", out)
	}

	log.Printf("🎉 %s successfully created and joined the cluster", containerName)
	return nil
}

func StartQuorumMonitor(rabbitAPIURL, user, pass string) {
	go func() {
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			log.Fatalf("cannot connect to docker daemon: %v", err)
		}
		defer cli.Close()

		for {
			updateExpectedNodes(context.Background(), cli) // 🆕 refresh node list
			checkQuorumStatus(context.Background(), cli, rabbitAPIURL, user, pass)
			time.Sleep(10 * time.Second)
		}
	}()
}

func checkQuorumStatus(ctx context.Context, cli *client.Client, url, user, pass string) {
	log.Println("🔍 [quorum_recovery] polling cluster status…")

	// Fetch live nodes from RabbitMQ management HTTP API
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

	// Compare expectedNodes (based on Docker) with live RabbitMQ nodes (based on API)
	for _, name := range expectedNodes {
		if !live[name] {
			log.Printf("⚠️ Node %s missing from /api/nodes", name)
			triggerRecovery(ctx, cli, name)
		}
	}

	for _, old := range previousExpectedNodes {
		found := false
		for _, curr := range expectedNodes {
			if old == curr {
				found = true
				break
			}
		}
		if !found && strings.HasPrefix(old, "rabbit@rabbit-recover") {
			log.Printf("🛑 Detected missing recovery container for %s — triggering recovery", old)
			triggerRecovery(ctx, cli, old)
		}
	}

	// Now validate quorum queue health
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
	parts := strings.Split(node, "@")
	if len(parts) == 2 {
		return parts[1] // extract "rabbit1", "rabbit2", "rabbit-recover-590" etc.
	}
	return ""
}

func triggerRecovery(ctx context.Context, cli *client.Client, deadNode string) {

	start := time.Now()
	monitor.QuorumRecoveriesTotal.Inc() // Count every recovery attempt

	defer func() {
		if r := recover(); r != nil {
			monitor.QuorumRecoveriesFailed.Inc()
			log.Printf("🔥 Panic during recovery: %v", r)
			return
		}
		monitor.QuorumRecoveryDuration.Observe(time.Since(start).Seconds())
	}()

	if deadNode == "" || forgotten[deadNode] {
		return
	}
	forgotten[deadNode] = true

	container := extractContainer(deadNode)
	if container == "" {

		// 1) Forget the dead node out of the cluster (so it vanishes from the UI)
		log.Printf("💀 forgetting %s (no Docker container mapping)…", deadNode)
		out, err := execInContainer(ctx, cli, "rabbit1",
			"rabbitmqctl", "--node", "rabbit@rabbit1",
			"forget_cluster_node", deadNode,
		)
		if err != nil {
			log.Printf("❌ could not forget %s: %v\n%s", deadNode, err, out)
			monitor.QuorumRecoveriesFailed.Inc() // <- log actual failure
		} else {
			log.Printf("✅ forgotten %s:\n%s", deadNode, out)
		}
		// Generate replacement name
		newID := rand.Intn(10000)
		newContainer := fmt.Sprintf("rabbit-recover-%d", newID)
		newNode := fmt.Sprintf("rabbit@%s", newContainer)

		log.Printf("🆕 Creating replacement for %s → %s", deadNode, newNode)
		if err := addTestNode(ctx, cli, newContainer, newNode, "secretcookie", "guest", "guest"); err != nil {
			log.Printf("❌ failed to create replacement: %v", err)
		}
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
			"rabbitmqctl", "--node", "rabbit@rabbit1", "forget_cluster_node", deadNode,
		)
		if err == nil {
			log.Printf("✅ forgotten %s:\n%s", deadNode, out)
			break
		}
		if i == maxRetries {
			log.Printf("❌ could not forget %s after %d tries: %v\n%s", deadNode, maxRetries, err, out)
			monitor.QuorumRecoveriesFailed.Inc() // <- log actual failure

			// Fallback: create a new recovery node
			newID := rand.Intn(10000)
			newContainer := fmt.Sprintf("rabbit-recover-%d", newID)
			newNode := fmt.Sprintf("rabbit@%s", newContainer)

			log.Printf("🆕 Fallback recovery: creating %s as replacement for %s", newNode, deadNode)
			if err := addTestNode(ctx, cli, newContainer, newNode, "secretcookie", "guest", "guest"); err != nil {
				log.Printf("❌ fallback creation failed: %v", err)
			}
			return
		}

		sleep := backoff + time.Duration(rand.Intn(500))*time.Millisecond
		log.Printf("⏳ retrying in %v…", sleep)
		time.Sleep(sleep)
		backoff *= 2

	}

	// 3) Delete old container and volume completely
	log.Printf("🧹 Removing container %s...", container)
	if err := cli.ContainerRemove(ctx, container, types.ContainerRemoveOptions{
		Force: true, RemoveVolumes: true,
	}); err != nil {
		log.Printf("❌ Failed to remove container %s: %v", container, err)
	}

	// Remove corresponding volume (optional but recommended)
	volumeName := fmt.Sprintf("%s-data", container)
	if err := cli.VolumeRemove(ctx, volumeName, true); err != nil {
		log.Printf("⚠️ Failed to remove volume %s: %v", volumeName, err)
	}

	// 4) Create new recovery container
	newID := rand.Intn(1000)
	newContainer := fmt.Sprintf("rabbit-recover-%d", newID)
	newNode := fmt.Sprintf("rabbit@%s", newContainer)

	log.Printf("🆕 Spawning new node to replace %s → %s", deadNode, newNode)
	if err := addTestNode(ctx, cli, newContainer, newNode, "secretcookie", "guest", "guest"); err != nil {
		log.Printf("❌ Failed to create recovery node: %v", err)
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
