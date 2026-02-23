#!/usr/bin/env bash
#
# a2a-trio-test.sh — Spawn 3 A2A agents, test collaboration + loop safety
#
# Tests:
#   1. A→B  direct call
#   2. A→B→C  chain (multi-hop)
#   3. A→B→A  loop detection (should NOT crash)
#   4. A→B + A→C  fan-out (parallel delegation)
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

PORT_A="${AGENT_A_PORT:-4015}"
PORT_B="${AGENT_B_PORT:-4016}"
PORT_C="${AGENT_C_PORT:-4017}"

WORK_DIR="/tmp/a2a-trio"
REGISTRY_DIR="/tmp/apteva-agents"
BINARY="$PROJECT_DIR/agent-core"

CONFIG_A="$WORK_DIR/agent-a.json"
CONFIG_B="$WORK_DIR/agent-b.json"
CONFIG_C="$WORK_DIR/agent-c.json"
LOG_A="$WORK_DIR/agent-a.log"
LOG_B="$WORK_DIR/agent-b.log"
LOG_C="$WORK_DIR/agent-c.log"
PID_A="$WORK_DIR/agent-a.pid"
PID_B="$WORK_DIR/agent-b.pid"
PID_C="$WORK_DIR/agent-c.pid"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

log()  { echo -e "${BLUE}[trio]${NC} $*"; }
ok()   { echo -e "${GREEN}[PASS]${NC} $*"; }
fail() { echo -e "${RED}[FAIL]${NC} $*"; }
info() { echo -e "${CYAN}[INFO]${NC} $*"; }

# Load .env
if [ -f "$PROJECT_DIR/.env" ]; then
    set -a; source "$PROJECT_DIR/.env"; set +a
fi

API_KEY="${AGENT_API_KEY:-agent-key}"

# -------------------------------------------------------------------
# Generate 3 agent configs
# -------------------------------------------------------------------
generate_configs() {
    mkdir -p "$WORK_DIR" "$REGISTRY_DIR"

    for agent in alpha bravo charlie; do
        case $agent in
            alpha)   name="Agent Alpha";   id="agent-alpha";   desc="First agent — coordinator and math expert" ;;
            bravo)   name="Agent Bravo";   id="agent-bravo";   desc="Second agent — creative writer" ;;
            charlie) name="Agent Charlie"; id="agent-charlie"; desc="Third agent — trivia and knowledge expert" ;;
        esac

        local cfg="$WORK_DIR/agent-${agent:0:1}.json"
        case $agent in
            alpha)   cfg="$CONFIG_A" ;;
            bravo)   cfg="$CONFIG_B" ;;
            charlie) cfg="$CONFIG_C" ;;
        esac

        cat > "$cfg" <<EOF
{
  "agent": {
    "id": "$id",
    "name": "$name",
    "description": "$desc",
    "llm": {
      "provider": "anthropic",
      "model": "claude-sonnet-4-5",
      "max_tokens": 4000,
      "temperature": 0.3,
      "system_prompt": "You are $name. You are part of a 3-agent system (Alpha, Bravo, Charlie). When told to ask or collaborate with another agent, ALWAYS use the call_agent tool. Be very concise — one or two sentences max. Never refuse to use call_agent when asked.",
      "tools": [],
      "builtin_tools": [],
      "vision": { "enabled": false },
      "parallel_tools": { "enabled": true, "max_concurrent": 10 }
    },
    "a2a": { "enabled": true, "push_notifications": false },
    "agents": {
      "enabled": true,
      "mode": "peer",
      "group": "trio-test",
      "discovery_method": "file",
      "file_registry_path": "/tmp/apteva-agents",
      "refresh_interval": "3s",
      "settings": {
        "default_timeout": "90s",
        "retry_count": 1,
        "retry_delay": "1s",
        "max_call_depth": 5,
        "track_call_chain": true,
        "call_ttl": "180s"
      }
    },
    "tasks": { "enabled": true, "allow_scheduling": false, "max_tasks": 50 },
    "mcp": { "enabled": false },
    "memory": { "enabled": false },
    "context": {
      "max_messages": 20,
      "max_tokens": 40000,
      "compaction": { "enabled": false }
    }
  }
}
EOF
    done

    log "Generated 3 agent configs"
}

# -------------------------------------------------------------------
# Start / stop
# -------------------------------------------------------------------
start_agents() {
    # Check ports
    for port in $PORT_A $PORT_B $PORT_C; do
        if lsof -i ":$port" -sTCP:LISTEN -t &>/dev/null 2>&1; then
            fail "Port $port is already in use"; exit 1
        fi
    done

    log "Building..."
    cd "$PROJECT_DIR"
    CGO_ENABLED=1 go build -o "$BINARY" .

    generate_configs

    # Clean old registry
    rm -f "$REGISTRY_DIR"/agent-alpha.json "$REGISTRY_DIR"/agent-bravo.json "$REGISTRY_DIR"/agent-charlie.json 2>/dev/null || true

    # Start all 3
    for triple in "Alpha:$PORT_A:$CONFIG_A:$LOG_A:$PID_A" \
                  "Bravo:$PORT_B:$CONFIG_B:$LOG_B:$PID_B" \
                  "Charlie:$PORT_C:$CONFIG_C:$LOG_C:$PID_C"; do
        IFS=: read -r name port cfg logf pidf <<< "$triple"
        log "Starting $name on port $port..."
        PORT=$port CONFIG_PATH="$cfg" ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" AGENT_API_KEY="$API_KEY" \
            AGENT_URL="http://localhost:$port" "$BINARY" > "$logf" 2>&1 &
        echo $! > "$pidf"
    done

    # Wait for health
    log "Waiting for all 3 agents..."
    for i in $(seq 1 30); do
        all_ok=true
        for port in $PORT_A $PORT_B $PORT_C; do
            curl -sf "http://localhost:$port/health" > /dev/null 2>&1 || all_ok=false
        done
        $all_ok && break
        sleep 1
    done

    for port in $PORT_A $PORT_B $PORT_C; do
        if ! curl -sf "http://localhost:$port/health" > /dev/null 2>&1; then
            fail "Agent on port $port failed to start"
            cat "$WORK_DIR"/*.log | tail -30
            stop_agents
            exit 1
        fi
    done

    ok "All 3 agents healthy"

    # Wait for discovery (need 3 agents to see each other)
    log "Waiting for peer discovery..."
    sleep 5
    ok "Discovery window complete"
}

stop_agents() {
    log "Stopping all agents..."
    for pidf in "$PID_A" "$PID_B" "$PID_C"; do
        if [ -f "$pidf" ]; then
            pid=$(cat "$pidf")
            if kill -0 "$pid" 2>/dev/null; then
                kill "$pid" 2>/dev/null || true
                for i in $(seq 1 5); do
                    kill -0 "$pid" 2>/dev/null || break
                    sleep 0.5
                done
                kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null || true
            fi
            rm -f "$pidf"
        fi
    done
    rm -f "$REGISTRY_DIR"/agent-alpha.json "$REGISTRY_DIR"/agent-bravo.json "$REGISTRY_DIR"/agent-charlie.json 2>/dev/null || true
    ok "All agents stopped"
}

# -------------------------------------------------------------------
# Helper: send message/send and extract result
# -------------------------------------------------------------------
a2a_send() {
    local port="$1"
    local text="$2"
    local timeout="${3:-90}"

    curl -s --max-time "$timeout" -X POST "http://localhost:$port/a2a" \
        -H "Content-Type: application/json" \
        -H "X-API-Key: $API_KEY" \
        -d "{
            \"jsonrpc\": \"2.0\",
            \"id\": \"test-$(date +%s%N)\",
            \"method\": \"message/send\",
            \"params\": {
                \"message\": {
                    \"role\": \"user\",
                    \"parts\": [{\"kind\": \"text\", \"text\": $(echo "$text" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().strip()))')}]
                }
            }
        }" 2>/dev/null || echo ""
}

# -------------------------------------------------------------------
# Tests
# -------------------------------------------------------------------
run_tests() {
    echo ""
    echo -e "${CYAN}╔═══════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║   A2A 3-Agent Collaboration Tests         ║${NC}"
    echo -e "${CYAN}╚═══════════════════════════════════════════╝${NC}"
    echo ""

    pass=0
    total=0

    # ── Test 1: Direct call  A → B ──────────────────────────────
    total=$((total + 1))
    echo -e "${BLUE}[Test $total]${NC} Direct call: Alpha → Bravo"
    info "  Asking Alpha to call Bravo for a joke..."

    resp=$(a2a_send $PORT_A "Use call_agent to ask agent-bravo: Tell me a one-line joke." 90)
    state=$(echo "$resp" | jq -r '.result.status.state // "error"' 2>/dev/null)
    text=$(echo "$resp" | jq -r '.result.artifacts[0].parts[0].text // "no response"' 2>/dev/null | head -c 300)

    if [ "$state" = "completed" ] && [ -n "$text" ] && [ "$text" != "no response" ]; then
        ok "  state=$state"
        info "  Alpha says: $text"
        pass=$((pass + 1))
    else
        fail "  state=$state"
        echo "  Response: $(echo "$resp" | head -c 300)"
    fi
    echo ""

    # ── Test 2: Chain  A → B → C ────────────────────────────────
    total=$((total + 1))
    echo -e "${BLUE}[Test $total]${NC} Chain call: Alpha → Bravo → Charlie"
    info "  Asking Alpha to ask Bravo to ask Charlie for a fun fact..."

    resp=$(a2a_send $PORT_A "Use call_agent to ask agent-bravo the following: Use call_agent to ask agent-charlie what is the capital of Iceland. Pass the answer back to me." 120)
    state=$(echo "$resp" | jq -r '.result.status.state // "error"' 2>/dev/null)
    text=$(echo "$resp" | jq -r '.result.artifacts[0].parts[0].text // "no response"' 2>/dev/null | head -c 400)

    if [ "$state" = "completed" ] && [ -n "$text" ] && [ "$text" != "no response" ]; then
        # Check if the answer made it back (Reykjavik should appear somewhere)
        if echo "$text" | grep -qi "reykjavik"; then
            ok "  state=$state — answer propagated through chain!"
        else
            ok "  state=$state — chain completed (answer may be paraphrased)"
        fi
        info "  Alpha says: $text"
        pass=$((pass + 1))
    else
        fail "  state=$state"
        echo "  Response: $(echo "$resp" | head -c 400)"
    fi
    echo ""

    # ── Test 3: Loop detection  A → B → A ───────────────────────
    total=$((total + 1))
    echo -e "${BLUE}[Test $total]${NC} Loop safety: Alpha → Bravo → Alpha (should NOT crash)"
    info "  Asking Alpha to ask Bravo to ask Alpha — loop guard should kick in..."

    resp=$(a2a_send $PORT_A "Use call_agent to ask agent-bravo the following: Use call_agent to ask agent-alpha what is 1+1." 90)
    state=$(echo "$resp" | jq -r '.result.status.state // "error"' 2>/dev/null)
    text=$(echo "$resp" | jq -r '.result.artifacts[0].parts[0].text // "no response"' 2>/dev/null | head -c 400)

    if [ "$state" = "completed" ]; then
        # Agent should have completed — either guard rail blocked the loop call or the LLM answered directly
        ok "  state=$state — no crash!"
        info "  Alpha says: $text"
        pass=$((pass + 1))
    elif [ "$state" = "failed" ]; then
        # Failed is also acceptable — as long as the server didn't crash
        # Verify agents are still alive
        a_alive=false; b_alive=false
        curl -sf "http://localhost:$PORT_A/health" > /dev/null 2>&1 && a_alive=true
        curl -sf "http://localhost:$PORT_B/health" > /dev/null 2>&1 && b_alive=true
        if $a_alive && $b_alive; then
            ok "  state=$state — loop was rejected, agents still alive"
            info "  Response: $text"
            pass=$((pass + 1))
        else
            fail "  Agent crashed! Alpha=$a_alive, Bravo=$b_alive"
        fi
    else
        # Even "error" — check if agents are still running
        a_alive=false; b_alive=false
        curl -sf "http://localhost:$PORT_A/health" > /dev/null 2>&1 && a_alive=true
        curl -sf "http://localhost:$PORT_B/health" > /dev/null 2>&1 && b_alive=true
        if $a_alive && $b_alive; then
            ok "  Agents survived (state=$state)"
            info "  Response: $text"
            pass=$((pass + 1))
        else
            fail "  Agent crashed! Alpha=$a_alive, Bravo=$b_alive"
        fi
    fi
    echo ""

    # ── Test 4: Fan-out  A → B + A → C ──────────────────────────
    total=$((total + 1))
    echo -e "${BLUE}[Test $total]${NC} Fan-out: Alpha calls both Bravo and Charlie"
    info "  Asking Alpha to ask both agents different questions..."

    resp=$(a2a_send $PORT_A "I need two things: 1) Use call_agent to ask agent-bravo to say hello in French. 2) Use call_agent to ask agent-charlie to say hello in Japanese. Give me both answers." 120)
    state=$(echo "$resp" | jq -r '.result.status.state // "error"' 2>/dev/null)
    text=$(echo "$resp" | jq -r '.result.artifacts[0].parts[0].text // "no response"' 2>/dev/null | head -c 400)

    if [ "$state" = "completed" ] && [ -n "$text" ] && [ "$text" != "no response" ]; then
        ok "  state=$state"
        info "  Alpha says: $text"
        pass=$((pass + 1))
    else
        fail "  state=$state"
        echo "  Response: $(echo "$resp" | head -c 400)"
    fi
    echo ""

    # ── Test 5: All agents still healthy after everything ────────
    total=$((total + 1))
    echo -e "${BLUE}[Test $total]${NC} Health check: all 3 agents still alive after tests"
    all_healthy=true
    for pair in "Alpha:$PORT_A" "Bravo:$PORT_B" "Charlie:$PORT_C"; do
        IFS=: read -r name port <<< "$pair"
        if curl -sf "http://localhost:$port/health" > /dev/null 2>&1; then
            info "  $name (port $port): healthy"
        else
            fail "  $name (port $port): DOWN"
            all_healthy=false
        fi
    done
    if $all_healthy; then
        ok "  All agents survived all tests"
        pass=$((pass + 1))
    fi

    # ── Summary ──────────────────────────────────────────────────
    echo ""
    echo -e "${CYAN}╔═══════════════════════════════════════════╗${NC}"
    if [ "$pass" -eq "$total" ]; then
        echo -e "${CYAN}║${NC}   ${GREEN}All $total/$total tests passed!${NC}                   ${CYAN}║${NC}"
    else
        echo -e "${CYAN}║${NC}   ${YELLOW}$pass/$total tests passed${NC}                        ${CYAN}║${NC}"
    fi
    echo -e "${CYAN}╚═══════════════════════════════════════════╝${NC}"
    echo ""

    [ "$pass" -eq "$total" ] && return 0 || return 1
}

# -------------------------------------------------------------------
# Main
# -------------------------------------------------------------------
trap 'stop_agents' EXIT INT TERM

start_agents
run_tests
