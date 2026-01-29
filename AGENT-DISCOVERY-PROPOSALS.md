# Agent Self-Discovery - P2P Architecture Proposals

## Problem Statement
Current approach requires manual configuration of `available_agents` array with hardcoded URLs, which is:
- ❌ Not scalable (manual config per agent)
- ❌ Static (can't discover new agents at runtime)
- ❌ Brittle (hardcoded URLs/ports)
- ❌ Centralized (need to know all agents upfront)
- ❌ No dynamic topology changes

## Design Goals
- ✅ Zero-config agent discovery
- ✅ Peer-to-peer (no central registry)
- ✅ Dynamic (agents join/leave at runtime)
- ✅ Local-first (work on localhost and LAN)
- ✅ Lightweight (no heavy dependencies)
- ✅ Resilient (handle failures gracefully)

---

## Proposal 1: mDNS/DNS-SD (Multicast DNS Service Discovery)

### Overview
Use mDNS (Bonjour/Avahi) for zero-config local network discovery. Agents broadcast their presence on the local network.

### How It Works
```
Agent A starts on :4015
  ↓
Broadcasts: "_agent._tcp.local" with:
  - Name: "Research Assistant"
  - Port: 4015
  - TXT records: capabilities=research,analysis

Agent B starts on :4016
  ↓
Broadcasts: "_agent._tcp.local"
  - Name: "Code Assistant"
  - Port: 4016
  - TXT records: capabilities=coding,python

Both agents continuously discover each other via mDNS
```

### Implementation
```go
// Using github.com/hashicorp/mdns

// Service registration
service, _ := mdns.NewMDNSService(
    "agent-research-001",
    "_agent._tcp",
    "",
    "",
    4015,
    nil,
    []string{
        "capabilities=research,analysis",
        "tags=planning",
        "version=1.0.0",
    },
)
server, _ := mdns.NewServer(&mdns.Config{Zone: service})

// Service discovery
entriesCh := make(chan *mdns.ServiceEntry, 10)
mdns.Lookup("_agent._tcp", entriesCh)

for entry := range entriesCh {
    // entry.Host, entry.Port, entry.InfoFields
    registerDiscoveredAgent(entry)
}
```

### Pros
✅ Industry standard (used by printers, AirPlay, etc.)
✅ Zero-config on local network
✅ Automatic discovery/removal
✅ Works across machines on same network
✅ TXT records for metadata (capabilities, tags)
✅ Lightweight protocol
✅ Library support: `github.com/hashicorp/mdns`

### Cons
❌ Local network only (not internet-scale)
❌ Requires UDP multicast (firewalls may block)
❌ Limited metadata in TXT records
❌ No authentication built-in
❌ DNS-SD not ideal for frequent changes

### Use Cases
- ✅ Local development
- ✅ Small team clusters
- ✅ LAN deployments
- ❌ Cloud/distributed systems

---

## Proposal 2: Gossip Protocol (SWIM/Memberlist)

### Overview
Agents form a peer-to-peer mesh using gossip protocol. Each agent knows a few peers, and information spreads through the network.

### How It Works
```
Agent A starts
  ↓
Joins cluster (needs 1 seed address)
  ↓
Gossips with peers: "I'm alive, I do research"
  ↓
Receives gossip: "Agent B does coding"
  ↓
Both agents have full cluster membership
```

### Implementation
```go
// Using github.com/hashicorp/memberlist

config := memberlist.DefaultLocalConfig()
config.Name = "agent-research-001"
config.BindPort = 7946
config.Metadata = []byte(`{"capabilities":["research"],"url":"http://localhost:4015"}`)

list, _ := memberlist.Create(config)

// Join with at least one known peer (can be env var)
if seedAddr := os.Getenv("AGENT_SEED"); seedAddr != "" {
    list.Join([]string{seedAddr})
}

// Get all members
for _, member := range list.Members() {
    // Parse metadata and register agent
    var metadata map[string]interface{}
    json.Unmarshal(member.Meta, &metadata)
    registerAgent(metadata)
}
```

### Pros
✅ True P2P (no central server needed)
✅ Fault-tolerant (survives node failures)
✅ Scales to hundreds of nodes
✅ Automatic failure detection
✅ Works across networks/internet
✅ Rich metadata support
✅ Battle-tested (Consul uses this)
✅ Only needs ONE seed address to join

### Cons
❌ Requires gossip port (7946 by default)
❌ Eventual consistency (slight delay in discovery)
❌ More complex than mDNS
❌ Needs seed address for first join

### Use Cases
- ✅ Cloud deployments
- ✅ Distributed systems
- ✅ Multi-datacenter
- ✅ Dynamic scaling
- ✅ Production environments

---

## Proposal 3: HTTP Beacon with Heartbeat

### Overview
Agents register themselves with a simple HTTP beacon endpoint. Each agent exposes `/agent-info` and pings known peers.

### How It Works
```
Agent A starts
  ↓
Exposes: GET /agent-info
  {
    "id": "agent-research-001",
    "name": "Research Assistant",
    "url": "http://localhost:4015",
    "capabilities": ["research"],
    "health": "healthy"
  }
  ↓
Periodically pings known peers:
  - Reads AGENT_PEERS env: "localhost:4016,localhost:4017"
  - Fetches /agent-info from each
  - Discovers new agents from their peer lists
  ↓
Builds dynamic agent registry
```

### Implementation
```go
// Standard endpoint
http.HandleFunc("/agent-info", handleAgentInfo)

func handleAgentInfo(w http.ResponseWriter, r *http.Request) {
    info := AgentInfo{
        ID:           cfg.Get().ID,
        Name:         cfg.Get().Name,
        URL:          fmt.Sprintf("http://localhost:%s", port),
        Capabilities: cfg.Get().Capabilities,
        Tags:         cfg.Get().Tags,
        Health:       "healthy",
        Peers:        getKnownPeers(), // Share peers!
        LastSeen:     time.Now(),
    }
    json.NewEncoder(w).Encode(info)
}

// Discovery ticker
func startDiscovery() {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        discoverPeers()
    }
}

func discoverPeers() {
    // Start with seed peers from env
    seeds := strings.Split(os.Getenv("AGENT_PEERS"), ",")

    discovered := make(map[string]AgentInfo)

    for _, peer := range seeds {
        info := fetchAgentInfo(peer)
        discovered[info.ID] = info

        // Also check their peers (transitive discovery)
        for _, secondDegree := range info.Peers {
            if _, exists := discovered[secondDegree]; !exists {
                info2 := fetchAgentInfo(secondDegree)
                discovered[info2.ID] = info2
            }
        }
    }

    updateAgentRegistry(discovered)
}
```

### Pros
✅ Simple HTTP (no special protocols)
✅ No extra dependencies
✅ Easy to debug (just curl)
✅ Transitive discovery (friend-of-friend)
✅ Works with existing HTTP server
✅ Can include health checks
✅ Peer list sharing enables full mesh discovery

### Cons
❌ Requires at least one seed peer
❌ Polling overhead (network traffic)
❌ Slower discovery (based on ticker interval)
❌ No push notifications

### Use Cases
- ✅ Simple deployments
- ✅ Behind NAT/firewalls
- ✅ Low node count (<20)
- ✅ Development/testing

---

## Proposal 4: Broadcast UDP Beacons

### Overview
Agents broadcast UDP packets on local network announcing their presence. Like mDNS but simpler.

### How It Works
```
Agent A starts on :4015
  ↓
Every 10s broadcasts UDP packet to 255.255.255.255:9999
  {
    "id": "agent-research-001",
    "port": 4015,
    "capabilities": ["research"]
  }
  ↓
Agent B receives broadcast
  ↓
Registers Agent A in local registry
  ↓
Agent B also broadcasts (Agent A discovers B)
```

### Implementation
```go
// Broadcast beacon
func broadcastPresence() {
    conn, _ := net.DialUDP("udp", nil, &net.UDPAddr{
        IP:   net.IPv4(255, 255, 255, 255),
        Port: 9999,
    })

    beacon := AgentBeacon{
        ID:           agentID,
        Port:         4015,
        Capabilities: capabilities,
        Timestamp:    time.Now(),
    }

    data, _ := json.Marshal(beacon)
    conn.Write(data)
}

// Listen for beacons
func listenForBeacons() {
    addr := net.UDPAddr{Port: 9999, IP: net.ParseIP("0.0.0.0")}
    conn, _ := net.ListenUDP("udp", &addr)

    buffer := make([]byte, 1024)
    for {
        n, _, _ := conn.ReadFrom(buffer)
        var beacon AgentBeacon
        json.Unmarshal(buffer[:n], &beacon)
        registerDiscoveredAgent(beacon)
    }
}
```

### Pros
✅ Extremely simple
✅ Fast discovery (real-time broadcasts)
✅ No central server
✅ Works on local network
✅ Minimal overhead

### Cons
❌ Local network only
❌ Broadcast storms on large networks
❌ Firewalls may block UDP
❌ No built-in authentication
❌ Limited metadata size (UDP packet limits)

### Use Cases
- ✅ Local development
- ✅ Small clusters (<10 agents)
- ✅ Fast discovery required
- ❌ Production/cloud

---

## Proposal 5: Shared SQLite Registry (File-based Discovery)

### Overview
Agents register themselves in a shared SQLite database file. Simple, no network needed.

### How It Works
```
Agent A starts
  ↓
Opens: /tmp/agents-registry.db
  ↓
Inserts/Updates:
  INSERT INTO agents (id, name, url, capabilities, last_seen)
  VALUES ('agent-research-001', 'Research', 'http://localhost:4015', '["research"]', NOW())
  ↓
Queries for other agents:
  SELECT * FROM agents WHERE last_seen > NOW() - 60
  ↓
Discovers Agent B (if it's been active in last 60s)
```

### Implementation
```go
// Shared registry
const registryPath = "/tmp/agents-registry.db"

// Register self
func registerSelf() {
    db, _ := sql.Open("sqlite3", registryPath)
    defer db.Close()

    db.Exec(`
        INSERT OR REPLACE INTO agents
        (id, name, url, capabilities, tags, last_seen)
        VALUES (?, ?, ?, ?, ?, ?)
    `, agentID, name, url, capsJSON, tagsJSON, time.Now())
}

// Discover others
func discoverAgents() {
    db, _ := sql.Open("sqlite3", registryPath)
    defer db.Close()

    rows, _ := db.Query(`
        SELECT id, name, url, capabilities, tags
        FROM agents
        WHERE last_seen > datetime('now', '-60 seconds')
        AND id != ?
    `, myAgentID)

    // Parse and register discovered agents
}

// Heartbeat ticker (every 30s)
func startHeartbeat() {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        registerSelf() // Update last_seen
        discoverAgents() // Check for new agents
    }
}
```

### Pros
✅ Extremely simple (just SQLite)
✅ No network protocols needed
✅ Works with containers (shared volume)
✅ Queryable (can use SQL for filtering)
✅ Transaction safety
✅ No ports/firewall issues
✅ Built-in TTL (last_seen timestamp)

### Cons
❌ Requires shared filesystem
❌ File locking contention with many agents
❌ Not suitable for distributed systems
❌ Doesn't work across networks

### Use Cases
- ✅ Single machine (multiple ports)
- ✅ Docker Compose with shared volume
- ✅ Development/testing
- ✅ Simple deployments
- ❌ Cloud/distributed

---

## Proposal 6: Redis Pub/Sub Discovery

### Overview
Use Redis as a lightweight message bus. Agents publish presence to a channel and subscribe to discover others.

### How It Works
```
Agent A starts
  ↓
Subscribes to: "agents:discovery"
  ↓
Publishes to "agents:discovery":
  {
    "type": "announce",
    "id": "agent-research-001",
    "url": "http://localhost:4015",
    "capabilities": ["research"]
  }
  ↓
Agent B publishes its announcement
  ↓
Agent A receives via subscription → registers Agent B
  ↓
Both agents also SET key with TTL:
  SETEX agents:agent-research-001 60 '{"url":"...","capabilities":[...]}'
  ↓
Can query all active agents:
  KEYS agents:*
```

### Implementation
```go
// Using github.com/redis/go-redis

// Subscribe to discovery channel
pubsub := redis.Subscribe("agents:discovery")
go func() {
    for msg := range pubsub.Channel() {
        var announcement AgentAnnouncement
        json.Unmarshal([]byte(msg.Payload), &announcement)

        if announcement.ID != myID {
            registerDiscoveredAgent(announcement)
        }
    }
}()

// Announce self
func announcePresence() {
    announcement := AgentAnnouncement{
        Type:         "announce",
        ID:           agentID,
        URL:          agentURL,
        Capabilities: capabilities,
        Timestamp:    time.Now(),
    }

    data, _ := json.Marshal(announcement)
    redis.Publish("agents:discovery", data)

    // Also set key with TTL for queries
    redis.SetEx(
        fmt.Sprintf("agents:%s", agentID),
        60 * time.Second,
        agentInfoJSON,
    )
}

// Query all active agents
func getActiveAgents() []AgentInfo {
    keys, _ := redis.Keys("agents:*").Result()
    for _, key := range keys {
        data, _ := redis.Get(key).Result()
        // Parse and collect agents
    }
}

// Heartbeat (every 30s)
ticker := time.NewTicker(30 * time.Second)
for range ticker.C {
    announcePresence()
}
```

### Pros
✅ Real-time discovery (pub/sub is instant)
✅ TTL-based expiry (auto-remove dead agents)
✅ Queryable (can SCAN/KEYS for filters)
✅ Scalable to hundreds of agents
✅ Works across networks
✅ Can add more features (presence, status)
✅ Single dependency (Redis)
✅ Push notifications (no polling)

### Cons
❌ Requires Redis server
❌ External dependency
❌ Single point of failure (unless Redis cluster)
❌ Not truly P2P (Redis is central)

### Use Cases
- ✅ Production environments
- ✅ Cloud deployments
- ✅ Large agent fleets (100+)
- ✅ Real-time updates critical
- ❌ Edge/offline scenarios

---

## Proposal 7: Hybrid - Local File + HTTP Discovery

### Overview
Combine file-based registry for localhost agents + HTTP polling for remote agents.

### How It Works
```
On localhost:
  ↓
Agents write to: ~/.apteva/agents.json
  {
    "agents": {
      "agent-research-001": {
        "url": "http://localhost:4015",
        "capabilities": ["research"],
        "last_seen": "2025-01-01T10:00:00Z"
      }
    }
  }
  ↓
Agent reads file every 30s to discover local peers

For remote agents:
  ↓
Agent pings remote discovery endpoints (from env):
  REMOTE_AGENTS=https://agent1.example.com,https://agent2.example.com
  ↓
GET https://agent1.example.com/agent-info
  ↓
Builds combined local + remote registry
```

### Implementation
```go
const localRegistryPath = "~/.apteva/agents.json"

// Register self locally
func registerLocal() {
    registry := loadRegistry(localRegistryPath)
    registry.Agents[myID] = AgentInfo{
        URL:          myURL,
        Capabilities: capabilities,
        LastSeen:     time.Now(),
    }
    saveRegistry(localRegistryPath, registry)
}

// Discover local agents
func discoverLocal() []AgentInfo {
    registry := loadRegistry(localRegistryPath)

    var agents []AgentInfo
    for id, info := range registry.Agents {
        if id == myID {
            continue
        }
        if time.Since(info.LastSeen) < 60*time.Second {
            agents = append(agents, info)
        }
    }
    return agents
}

// Discover remote agents
func discoverRemote() []AgentInfo {
    remotes := strings.Split(os.Getenv("REMOTE_AGENTS"), ",")
    var agents []AgentInfo

    for _, remote := range remotes {
        info := fetchAgentInfo(remote + "/agent-info")
        agents = append(agents, info)
    }
    return agents
}

// Combined discovery
func discoverAll() {
    local := discoverLocal()
    remote := discoverRemote()
    updateRegistry(append(local, remote...))
}
```

### Pros
✅ Simple for local development
✅ Supports remote agents when needed
✅ No network protocol for localhost
✅ File is human-readable/editable
✅ Gradual migration (local → remote)
✅ No extra dependencies

### Cons
❌ File locking issues with many agents
❌ Remote polling overhead
❌ Mixed architecture (complex)
❌ Still needs seed URLs for remote

### Use Cases
- ✅ Development (localhost)
- ✅ Hybrid deployments (some local, some remote)
- ✅ Migration path from simple to complex

---

## Proposal 8: Environment Variable Discovery

### Overview
Simple convention: Agents discover peers through environment variables with a standardized format.

### How It Works
```bash
# Agent A
AGENT_ID=agent-research-001
AGENT_PORT=4015
AGENT_PEERS=localhost:4016,localhost:4017

# Agent B
AGENT_ID=agent-code-001
AGENT_PORT=4016
AGENT_PEERS=localhost:4015,localhost:4017

# Each agent:
1. Fetches /agent-info from all peers in AGENT_PEERS
2. Discovers new agents from their peer lists
3. Builds full mesh over time
```

### Implementation
```go
// Parse peers from env
peers := strings.Split(os.Getenv("AGENT_PEERS"), ",")

// Discover via HTTP
for _, peer := range peers {
    url := fmt.Sprintf("http://%s/agent-info", peer)
    info := fetchAgentInfo(url)
    registerAgent(info)

    // Transitive discovery
    for _, secondDegree := range info.Peers {
        if !isKnown(secondDegree) {
            transitiveInfo := fetchAgentInfo(secondDegree)
            registerAgent(transitiveInfo)
        }
    }
}
```

### Pros
✅ Extremely simple
✅ No dependencies
✅ Works everywhere (localhost, cloud, docker)
✅ Easy to configure
✅ Transitive discovery (mesh builds automatically)
✅ Human-readable

### Cons
❌ Still requires some manual config
❌ Polling-based (not real-time)
❌ No automatic cleanup of dead agents

### Use Cases
- ✅ All environments
- ✅ Quick prototyping
- ✅ Kubernetes (via env injection)
- ✅ Docker Compose

---

## Proposal 9: Docker DNS Discovery

### Overview
For Docker/Kubernetes environments, use built-in service discovery.

### How It Works
```yaml
# docker-compose.yml
services:
  agent-research:
    image: agent-go
    environment:
      AGENT_SERVICE_DISCOVERY: docker
      DOCKER_NETWORK: apteva-agents
    networks:
      - apteva-agents

  agent-code:
    image: agent-go
    environment:
      AGENT_SERVICE_DISCOVERY: docker
      DOCKER_NETWORK: apteva-agents
    networks:
      - apteva-agents

# Agents auto-discover each other via Docker DNS
```

### Implementation
```go
// List all containers on same network
func discoverDockerAgents() {
    // Use Docker API or DNS lookups
    network := os.Getenv("DOCKER_NETWORK")

    // Query Docker DNS for service names
    // agent-research.apteva-agents
    // agent-code.apteva-agents

    // Or use Docker API to list containers
    containers := dockerClient.ContainerList(...)
    for _, container := range containers {
        if hasLabel(container, "agent.enabled") {
            registerAgent(container)
        }
    }
}
```

### Pros
✅ Native to Docker/K8s
✅ Zero config in containerized environments
✅ Automatic DNS resolution
✅ Works with service mesh

### Cons
❌ Only works in Docker/K8s
❌ Requires Docker API access
❌ Not portable to non-container deployments

### Use Cases
- ✅ Docker Compose deployments
- ✅ Kubernetes clusters
- ❌ Bare metal
- ❌ Development (localhost)

---

## Recommendation Matrix

| Proposal | Complexity | Dependencies | Network | P2P | Best For |
|----------|-----------|--------------|---------|-----|----------|
| 1. mDNS | Low | mdns lib | LAN only | ✅ Yes | Local dev, LAN |
| 2. Gossip | Medium | memberlist | Internet | ✅ Yes | Production, cloud |
| 3. HTTP Beacon | Low | None | Internet | ⚠️ Hybrid | Simple deployments |
| 4. UDP Broadcast | Low | None | LAN only | ✅ Yes | Local dev |
| 5. SQLite File | Very Low | None | Filesystem | ❌ No | Localhost only |
| 6. Redis Pub/Sub | Medium | Redis | Internet | ⚠️ Central | Production |
| 7. Hybrid File+HTTP | Low | None | Both | ⚠️ Mixed | Migration path |
| 8. Env Vars + HTTP | Very Low | None | Internet | ⚠️ Hybrid | Universal |
| 9. Docker DNS | Low | Docker | Container | ✅ Yes | Containerized |

---

## My Top 3 Recommendations

### 🥇 **For You: Gossip Protocol (Proposal 2)**
**Why:**
- True P2P mesh network
- Only needs ONE seed address: `AGENT_SEED=localhost:4015`
- Auto-discovers entire cluster
- Production-ready (used by Consul, Nomad)
- Works localhost AND cloud
- Fault-tolerant

**Setup:**
```bash
# Agent A (seed)
AGENT_ID=agent-research-001 AGENT_PORT=4015 ./agent-go

# Agent B (joins via seed)
AGENT_ID=agent-code-001 AGENT_PORT=4016 AGENT_SEED=localhost:4015 ./agent-go

# Agent C (joins via any peer)
AGENT_ID=agent-data-001 AGENT_PORT=4017 AGENT_SEED=localhost:4016 ./agent-go

# All 3 agents now know about each other!
```

### 🥈 **Runner-up: mDNS (Proposal 1)**
**Why:**
- Zero config for local development
- Industry standard
- Works immediately on same network
- Good for demos/prototyping

**Limitation:** LAN only

### 🥉 **Simple Alternative: Env Vars + HTTP (Proposal 8)**
**Why:**
- Dead simple
- No dependencies
- Works everywhere
- Transitive discovery = eventual full mesh

**Limitation:** Requires initial seed peers

---

## Hybrid Recommendation: Gossip + mDNS

**Best of both worlds:**
1. Use **mDNS** for automatic local discovery (no config)
2. Use **Gossip** for cross-network discovery (with seeds)
3. Agents automatically join via mDNS if on same LAN
4. Can also join via explicit seed for remote agents

```go
// Start both discovery methods
go startMDNSDiscovery()  // Finds local agents automatically
go startGossipDiscovery() // Joins cluster via seed (optional)

// If AGENT_SEED set → use gossip for remote
// If on LAN → mDNS discovers locally
// Agents join both ways!
```

---

## Implementation Effort

| Proposal | Lines of Code | Time | Dependencies |
|----------|---------------|------|--------------|
| 1. mDNS | ~200 | 4-6 hours | `hashicorp/mdns` |
| 2. Gossip | ~300 | 6-8 hours | `hashicorp/memberlist` |
| 3. HTTP Beacon | ~150 | 3-4 hours | None |
| 4. UDP Broadcast | ~100 | 2-3 hours | None |
| 5. SQLite File | ~80 | 2 hours | None (has SQLite) |
| 6. Redis | ~200 | 4-5 hours | Redis + client lib |
| 7. Hybrid | ~250 | 5-6 hours | None |
| 8. Env Vars | ~120 | 2-3 hours | None |
| 9. Docker DNS | ~150 | 3-4 hours | Docker SDK |

---

## My Strong Recommendation

**Go with Gossip Protocol (Proposal 2)** because:

1. **True P2P** - No single point of failure
2. **Scalable** - Works for 1 agent or 1000
3. **Battle-tested** - HashiCorp's Consul uses this
4. **Simple config** - Just one seed address
5. **Auto-healing** - Detects and removes dead agents
6. **Works everywhere** - Localhost, LAN, cloud
7. **Future-proof** - Can add features like leader election later

### Example UX:
```bash
# First agent (becomes seed)
./agent-go

# Second agent
AGENT_SEED=localhost:7946 ./agent-go

# Third agent (can use any peer as seed)
AGENT_SEED=localhost:7947 ./agent-go

# All agents discover each other automatically!
```

No JSON config needed. Just start agents with one env var. 🎯

---

## Alternative: Keep It Simple (Proposal 8)

If you want minimal complexity:
- Use **Env Vars + HTTP** with transitive discovery
- Dead simple, no dependencies
- Just fetch `/agent-info` from seed peers
- Agents share their peer lists → mesh forms automatically

---

## Questions to Decide:

1. **Deployment model?** (localhost only, LAN, cloud, mix?)
2. **Agent count?** (2-5 agents, or planning for 50+?)
3. **Dependencies okay?** (willing to add libraries, or prefer zero-dep?)
4. **Discovery speed?** (real-time important, or 30s delay okay?)
5. **Network topology?** (all localhost, across machines, or cloud?)

Let me know your preferences and I can refine the recommendation!
