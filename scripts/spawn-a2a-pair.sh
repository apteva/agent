#!/usr/bin/env bash
#
# spawn-a2a-pair.sh — Start two A2A-enabled agents that discover each other
#
# Usage:
#   ./scripts/spawn-a2a-pair.sh              # Start both agents
#   ./scripts/spawn-a2a-pair.sh stop         # Stop both agents
#   ./scripts/spawn-a2a-pair.sh status       # Check status
#   ./scripts/spawn-a2a-pair.sh test         # Run A2A test calls
#   ./scripts/spawn-a2a-pair.sh logs [a|b]   # Tail logs
#
# Environment:
#   ANTHROPIC_API_KEY  — Required (or set in .env)
#   AGENT_A_PORT       — Port for Agent A (default: 4015)
#   AGENT_B_PORT       — Port for Agent B (default: 4016)
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Ports
PORT_A="${AGENT_A_PORT:-4015}"
PORT_B="${AGENT_B_PORT:-4016}"

# Working directories
WORK_DIR="/tmp/a2a-test"
REGISTRY_DIR="/tmp/apteva-agents"
CONFIG_A="$WORK_DIR/agent-a-config.json"
CONFIG_B="$WORK_DIR/agent-b-config.json"
LOG_A="$WORK_DIR/agent-a.log"
LOG_B="$WORK_DIR/agent-b.log"
PID_A="$WORK_DIR/agent-a.pid"
PID_B="$WORK_DIR/agent-b.pid"
BINARY="$PROJECT_DIR/agent-core"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

log()  { echo -e "${BLUE}[a2a]${NC} $*"; }
ok()   { echo -e "${GREEN}[OK]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
err()  { echo -e "${RED}[ERR]${NC} $*"; }

# Load .env if present
load_env() {
    if [ -f "$PROJECT_DIR/.env" ]; then
        set -a
        source "$PROJECT_DIR/.env"
        set +a
    fi
}

# Check prerequisites
check_prereqs() {
    load_env

    if [ -z "${ANTHROPIC_API_KEY:-}" ]; then
        err "ANTHROPIC_API_KEY not set. Export it or add to $PROJECT_DIR/.env"
        exit 1
    fi

    # Check if ports are free
    for port in $PORT_A $PORT_B; do
        if lsof -i ":$port" -sTCP:LISTEN -t &>/dev/null 2>&1; then
            err "Port $port is already in use"
            exit 1
        fi
    done
}

# Build the binary
build() {
    log "Building agent binary..."
    cd "$PROJECT_DIR"
    CGO_ENABLED=1 go build -o "$BINARY" .
    ok "Built $BINARY"
}

# Generate config files
generate_configs() {
    mkdir -p "$WORK_DIR"
    mkdir -p "$REGISTRY_DIR"

    # Agent A config
    cat > "$CONFIG_A" <<'AGENT_A'
{
  "agent": {
    "id": "agent-alpha",
    "name": "Agent Alpha",
    "description": "First A2A peer agent — good at math and logic",
    "llm": {
      "provider": "anthropic",
      "model": "claude-sonnet-4-5",
      "max_tokens": 4000,
      "temperature": 0.7,
      "system_prompt": "You are Agent Alpha, a helpful AI assistant. You are part of a multi-agent system. When asked, you can collaborate with other agents using the call_agent tool. Be concise.",
      "tools": [],
      "builtin_tools": [],
      "vision": {
        "enabled": true,
        "max_images": 20,
        "max_image_size": 5242880,
        "allowed_types": ["jpeg", "png", "gif", "webp"],
        "resize_images": true,
        "max_dimension": 1568,
        "pdf": { "enabled": true, "max_file_size": 33554432, "max_pages": 100, "allow_urls": true }
      },
      "parallel_tools": { "enabled": true, "max_concurrent": 10 }
    },
    "a2a": {
      "enabled": true,
      "push_notifications": false
    },
    "agents": {
      "enabled": true,
      "mode": "peer",
      "group": "a2a-test",
      "discovery_method": "file",
      "file_registry_path": "/tmp/apteva-agents",
      "refresh_interval": "3s",
      "settings": {
        "default_timeout": "60s",
        "retry_count": 2,
        "retry_delay": "1s",
        "max_call_depth": 3,
        "track_call_chain": true,
        "call_ttl": "120s"
      }
    },
    "tasks": { "enabled": true, "allow_scheduling": false, "max_tasks": 50 },
    "mcp": { "enabled": false },
    "memory": { "enabled": false },
    "context": {
      "max_messages": 30,
      "max_tokens": 80000,
      "compaction": { "enabled": true, "trigger_ratio": 0.7, "keep_recent": 15, "summary_max_tokens": 1000, "model": "claude-haiku-4-5" }
    }
  }
}
AGENT_A

    # Agent B config
    cat > "$CONFIG_B" <<'AGENT_B'
{
  "agent": {
    "id": "agent-bravo",
    "name": "Agent Bravo",
    "description": "Second A2A peer agent — good at writing and creativity",
    "llm": {
      "provider": "anthropic",
      "model": "claude-sonnet-4-5",
      "max_tokens": 4000,
      "temperature": 0.7,
      "system_prompt": "You are Agent Bravo, a creative AI assistant. You are part of a multi-agent system. When asked, you can collaborate with other agents using the call_agent tool. Be concise.",
      "tools": [],
      "builtin_tools": [],
      "vision": {
        "enabled": true,
        "max_images": 20,
        "max_image_size": 5242880,
        "allowed_types": ["jpeg", "png", "gif", "webp"],
        "resize_images": true,
        "max_dimension": 1568,
        "pdf": { "enabled": true, "max_file_size": 33554432, "max_pages": 100, "allow_urls": true }
      },
      "parallel_tools": { "enabled": true, "max_concurrent": 10 }
    },
    "a2a": {
      "enabled": true,
      "push_notifications": false
    },
    "agents": {
      "enabled": true,
      "mode": "peer",
      "group": "a2a-test",
      "discovery_method": "file",
      "file_registry_path": "/tmp/apteva-agents",
      "refresh_interval": "3s",
      "settings": {
        "default_timeout": "60s",
        "retry_count": 2,
        "retry_delay": "1s",
        "max_call_depth": 3,
        "track_call_chain": true,
        "call_ttl": "120s"
      }
    },
    "tasks": { "enabled": true, "allow_scheduling": false, "max_tasks": 50 },
    "mcp": { "enabled": false },
    "memory": { "enabled": false },
    "context": {
      "max_messages": 30,
      "max_tokens": 80000,
      "compaction": { "enabled": true, "trigger_ratio": 0.7, "keep_recent": 15, "summary_max_tokens": 1000, "model": "claude-haiku-4-5" }
    }
  }
}
AGENT_B

    ok "Generated configs: $CONFIG_A, $CONFIG_B"
}

# Start agents
start_agents() {
    check_prereqs
    build
    generate_configs

    # Clean stale registry entries
    rm -f "$REGISTRY_DIR"/agent-alpha.json "$REGISTRY_DIR"/agent-bravo.json 2>/dev/null || true

    log "Starting Agent Alpha on port $PORT_A..."
    cd "$PROJECT_DIR"
    PORT=$PORT_A \
    CONFIG_PATH="$CONFIG_A" \
    ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
    AGENT_URL="http://localhost:$PORT_A" \
        "$BINARY" > "$LOG_A" 2>&1 &
    echo $! > "$PID_A"
    ok "Agent Alpha started (PID $(cat "$PID_A"))"

    log "Starting Agent Bravo on port $PORT_B..."
    PORT=$PORT_B \
    CONFIG_PATH="$CONFIG_B" \
    ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
    AGENT_URL="http://localhost:$PORT_B" \
        "$BINARY" > "$LOG_B" 2>&1 &
    echo $! > "$PID_B"
    ok "Agent Bravo started (PID $(cat "$PID_B"))"

    # Wait for both to be healthy
    log "Waiting for agents to be ready..."
    for i in $(seq 1 30); do
        a_ok=false
        b_ok=false
        curl -sf "http://localhost:$PORT_A/health" > /dev/null 2>&1 && a_ok=true
        curl -sf "http://localhost:$PORT_B/health" > /dev/null 2>&1 && b_ok=true
        if $a_ok && $b_ok; then
            break
        fi
        sleep 1
    done

    if ! curl -sf "http://localhost:$PORT_A/health" > /dev/null 2>&1; then
        err "Agent Alpha failed to start. Check $LOG_A"
        exit 1
    fi
    if ! curl -sf "http://localhost:$PORT_B/health" > /dev/null 2>&1; then
        err "Agent Bravo failed to start. Check $LOG_B"
        exit 1
    fi

    ok "Both agents are healthy!"

    # Wait a moment for discovery to run
    sleep 4

    echo ""
    echo -e "${CYAN}=========================================${NC}"
    echo -e "${CYAN}  A2A Agent Pair Ready${NC}"
    echo -e "${CYAN}=========================================${NC}"
    echo ""
    echo -e "  Agent Alpha: ${GREEN}http://localhost:$PORT_A${NC}"
    echo -e "  Agent Bravo: ${GREEN}http://localhost:$PORT_B${NC}"
    echo ""
    echo -e "  Agent Cards:"
    echo -e "    curl http://localhost:$PORT_A/.well-known/agent.json | jq ."
    echo -e "    curl http://localhost:$PORT_B/.well-known/agent.json | jq ."
    echo ""
    echo -e "  A2A message/send (Alpha → Bravo via A2A):"
    echo -e "    curl -s -X POST http://localhost:$PORT_B/a2a \\"
    echo -e "      -H 'Content-Type: application/json' \\"
    echo -e "      -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"message/send\",\"params\":{\"message\":{\"role\":\"user\",\"parts\":[{\"kind\":\"text\",\"text\":\"Hello!\"}]}}}' | jq ."
    echo ""
    echo -e "  Logs:"
    echo -e "    tail -f $LOG_A"
    echo -e "    tail -f $LOG_B"
    echo ""
    echo -e "  Stop:  ${YELLOW}./scripts/spawn-a2a-pair.sh stop${NC}"
    echo -e "  Test:  ${YELLOW}./scripts/spawn-a2a-pair.sh test${NC}"
    echo ""
}

# Stop agents
stop_agents() {
    log "Stopping agents..."

    for pidfile in "$PID_A" "$PID_B"; do
        if [ -f "$pidfile" ]; then
            pid=$(cat "$pidfile")
            if kill -0 "$pid" 2>/dev/null; then
                kill "$pid" 2>/dev/null || true
                # Wait for graceful shutdown
                for i in $(seq 1 5); do
                    kill -0 "$pid" 2>/dev/null || break
                    sleep 1
                done
                # Force kill if still alive
                kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null
                ok "Stopped PID $pid"
            else
                warn "PID $pid not running"
            fi
            rm -f "$pidfile"
        fi
    done

    # Clean registry
    rm -f "$REGISTRY_DIR"/agent-alpha.json "$REGISTRY_DIR"/agent-bravo.json 2>/dev/null || true

    ok "All agents stopped"
}

# Check status
status() {
    echo -e "${CYAN}Agent Status${NC}"
    echo ""

    for name in Alpha Bravo; do
        if [ "$name" = "Alpha" ]; then
            port=$PORT_A; pidfile=$PID_A
        else
            port=$PORT_B; pidfile=$PID_B
        fi

        pid="?"
        [ -f "$pidfile" ] && pid=$(cat "$pidfile")

        if curl -sf "http://localhost:$port/health" > /dev/null 2>&1; then
            health=$(curl -sf "http://localhost:$port/health")
            echo -e "  Agent $name (port $port, PID $pid): ${GREEN}HEALTHY${NC}"
            echo "    $health"
        else
            echo -e "  Agent $name (port $port, PID $pid): ${RED}DOWN${NC}"
        fi
    done

    echo ""

    # Check discovery
    echo -e "${CYAN}Discovery Registry ($REGISTRY_DIR)${NC}"
    if ls "$REGISTRY_DIR"/*.json &>/dev/null 2>&1; then
        for f in "$REGISTRY_DIR"/*.json; do
            agent_name=$(jq -r '.name // .id' "$f" 2>/dev/null || basename "$f")
            agent_url=$(jq -r '.url' "$f" 2>/dev/null || echo "?")
            echo "  - $agent_name → $agent_url"
        done
    else
        echo "  (no agents registered)"
    fi
}

# Ensure agents are running, start them if not
ensure_running() {
    local need_start=false
    curl -sf "http://localhost:$PORT_A/health" > /dev/null 2>&1 || need_start=true
    curl -sf "http://localhost:$PORT_B/health" > /dev/null 2>&1 || need_start=true

    if $need_start; then
        log "Agents not running — starting them first..."
        start_agents
    else
        ok "Agents already running"
    fi
}

# Run A2A test calls
run_tests() {
    load_env
    ensure_running

    echo ""
    echo -e "${CYAN}=========================================${NC}"
    echo -e "${CYAN}  A2A Protocol Tests${NC}"
    echo -e "${CYAN}=========================================${NC}"
    echo ""

    pass=0
    fail=0

    # API key for authenticated endpoints
    API_KEY="${AGENT_API_KEY:-agent-key}"
    AUTH_HEADER="X-API-Key: $API_KEY"

    # Test 1: Agent Cards
    echo -e "${BLUE}[1/6]${NC} Agent Card — Alpha"
    card_a=$(curl -sf "http://localhost:$PORT_A/.well-known/agent.json" 2>/dev/null || echo "")
    if [ -n "$card_a" ] && echo "$card_a" | jq -e '.name' > /dev/null 2>&1; then
        name=$(echo "$card_a" | jq -r '.name')
        skills=$(echo "$card_a" | jq '.skills | length')
        ok "  name=$name, skills=$skills, streaming=$(echo "$card_a" | jq '.capabilities.streaming')"
        pass=$((pass + 1))
    else
        err "  Failed to fetch agent card"
        fail=$((fail + 1))
    fi

    echo -e "${BLUE}[2/6]${NC} Agent Card — Bravo"
    card_b=$(curl -sf "http://localhost:$PORT_B/.well-known/agent.json" 2>/dev/null || echo "")
    if [ -n "$card_b" ] && echo "$card_b" | jq -e '.name' > /dev/null 2>&1; then
        name=$(echo "$card_b" | jq -r '.name')
        ok "  name=$name"
        pass=$((pass + 1))
    else
        err "  Failed to fetch agent card"
        fail=$((fail + 1))
    fi

    # Test 3: message/send to Alpha
    echo -e "${BLUE}[3/6]${NC} message/send → Alpha"
    send_resp=$(curl -sf -X POST "http://localhost:$PORT_A/a2a" \
        -H "Content-Type: application/json" \
        -H "$AUTH_HEADER" \
        -d '{
            "jsonrpc": "2.0",
            "id": "test-1",
            "method": "message/send",
            "params": {
                "message": {
                    "role": "user",
                    "parts": [{"kind": "text", "text": "What is 2+2? Reply with just the number."}]
                },
                "contextId": "test-context-1"
            }
        }' 2>/dev/null || echo "")

    if [ -n "$send_resp" ] && echo "$send_resp" | jq -e '.result.status.state' > /dev/null 2>&1; then
        state=$(echo "$send_resp" | jq -r '.result.status.state')
        task_id=$(echo "$send_resp" | jq -r '.result.id')
        response_text=$(echo "$send_resp" | jq -r '.result.artifacts[0].parts[0].text // "no text"' | head -c 100)
        ok "  state=$state, taskID=$task_id"
        ok "  response: $response_text"
        pass=$((pass + 1))
    else
        err "  message/send failed"
        echo "  Response: $send_resp" | head -c 200
        fail=$((fail + 1))
    fi

    # Test 4: message/send to Bravo
    echo -e "${BLUE}[4/6]${NC} message/send → Bravo"
    send_resp_b=$(curl -sf -X POST "http://localhost:$PORT_B/a2a" \
        -H "Content-Type: application/json" \
        -H "$AUTH_HEADER" \
        -d '{
            "jsonrpc": "2.0",
            "id": "test-2",
            "method": "message/send",
            "params": {
                "message": {
                    "role": "user",
                    "parts": [{"kind": "text", "text": "Write a haiku about computers. Just the haiku, nothing else."}]
                }
            }
        }' 2>/dev/null || echo "")

    if [ -n "$send_resp_b" ] && echo "$send_resp_b" | jq -e '.result.status.state' > /dev/null 2>&1; then
        state=$(echo "$send_resp_b" | jq -r '.result.status.state')
        task_id_b=$(echo "$send_resp_b" | jq -r '.result.id')
        response_text=$(echo "$send_resp_b" | jq -r '.result.artifacts[0].parts[0].text // "no text"' | head -c 200)
        ok "  state=$state, taskID=$task_id_b"
        ok "  response: $response_text"
        pass=$((pass + 1))
    else
        err "  message/send failed"
        fail=$((fail + 1))
    fi

    # Test 5: tasks/get (retrieve Alpha's task)
    echo -e "${BLUE}[5/6]${NC} tasks/get — retrieve task from Alpha"
    if [ -n "${task_id:-}" ] && [ "$task_id" != "null" ]; then
        get_resp=$(curl -sf -X POST "http://localhost:$PORT_A/a2a" \
            -H "Content-Type: application/json" \
            -H "$AUTH_HEADER" \
            -d "{
                \"jsonrpc\": \"2.0\",
                \"id\": \"test-3\",
                \"method\": \"tasks/get\",
                \"params\": {\"id\": \"$task_id\"}
            }" 2>/dev/null || echo "")

        if [ -n "$get_resp" ] && echo "$get_resp" | jq -e '.result.id' > /dev/null 2>&1; then
            state=$(echo "$get_resp" | jq -r '.result.status.state')
            hist_len=$(echo "$get_resp" | jq '.result.history | length')
            ok "  state=$state, history=$hist_len messages"
            pass=$((pass + 1))
        else
            err "  tasks/get failed"
            fail=$((fail + 1))
        fi
    else
        warn "  Skipped (no task ID from previous test)"
        fail=$((fail + 1))
    fi

    # Test 6: message/stream to Alpha
    echo -e "${BLUE}[6/6]${NC} message/stream → Alpha (SSE)"
    stream_resp=$(curl -sf -N -X POST "http://localhost:$PORT_A/a2a" \
        -H "Content-Type: application/json" \
        -H "$AUTH_HEADER" \
        --max-time 60 \
        -d '{
            "jsonrpc": "2.0",
            "id": "test-stream",
            "method": "message/stream",
            "params": {
                "message": {
                    "role": "user",
                    "parts": [{"kind": "text", "text": "Say hello in one word."}]
                }
            }
        }' 2>/dev/null || echo "")

    if [ -n "$stream_resp" ]; then
        event_count=$(echo "$stream_resp" | grep -c "^data: " || true)
        has_completed=$(echo "$stream_resp" | grep -c '"completed"' || true)
        if [ "$event_count" -gt 0 ] && [ "$has_completed" -gt 0 ]; then
            ok "  Received $event_count SSE events, stream completed"
            pass=$((pass + 1))
        else
            warn "  Got response but missing events (events=$event_count, completed=$has_completed)"
            fail=$((fail + 1))
        fi
    else
        err "  message/stream failed"
        fail=$((fail + 1))
    fi

    # Summary
    echo ""
    echo -e "${CYAN}=========================================${NC}"
    total=$((pass + fail))
    if [ "$fail" -eq 0 ]; then
        echo -e "  ${GREEN}All $total tests passed!${NC}"
    else
        echo -e "  ${GREEN}$pass passed${NC}, ${RED}$fail failed${NC} (of $total)"
    fi
    echo -e "${CYAN}=========================================${NC}"
    echo ""
}

# Tail logs
tail_logs() {
    agent="${1:-}"
    case "$agent" in
        a|alpha|A)
            tail -f "$LOG_A"
            ;;
        b|bravo|B)
            tail -f "$LOG_B"
            ;;
        *)
            tail -f "$LOG_A" "$LOG_B"
            ;;
    esac
}

# Main
case "${1:-start}" in
    start)
        start_agents
        ;;
    stop)
        stop_agents
        ;;
    restart)
        stop_agents
        sleep 1
        start_agents
        ;;
    status)
        status
        ;;
    test)
        run_tests
        ;;
    logs)
        tail_logs "${2:-}"
        ;;
    build)
        build
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status|test|logs [a|b]|build}"
        exit 1
        ;;
esac
