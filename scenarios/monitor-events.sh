#!/bin/bash

# Real-time event monitor using SSE streams from both agents
# Usage: ./monitor-events.sh PORT1 PORT2 LABEL1 LABEL2

PORT1=${1:-4015}
PORT2=${2:-4016}
LABEL1=${3:-"Coordinator"}
LABEL2=${4:-"Worker"}

# Colors
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# Function to format and display events
format_event() {
    local agent=$1
    local color=$2
    local line=$3

    # Parse JSON event
    if [[ "$line" =~ ^data:\ \{.*\}$ ]]; then
        # Extract data part
        data="${line#data: }"

        # Extract fields using basic parsing
        category=$(echo "$data" | grep -o '"category":"[^"]*"' | cut -d'"' -f4)
        type=$(echo "$data" | grep -o '"type":"[^"]*"' | cut -d'"' -f4)
        tool_name=$(echo "$data" | grep -o '"tool_name":"[^"]*"' | cut -d'"' -f4)
        agent_name=$(echo "$data" | grep -o '"agent_name":"[^"]*"' | cut -d'"' -f4)
        duration=$(echo "$data" | grep -o '"duration_ms":[0-9]*' | cut -d':' -f2)

        # Format based on event type
        case "$type" in
            message_received)
                echo -e "  ${color}[${agent}]${NC} 📨 Chat message received"
                ;;
            agent_call_started)
                echo -e "  ${color}[${agent}]${NC} 🚀 Calling agent: ${agent_name}"
                ;;
            agent_call_completed)
                echo -e "  ${color}[${agent}]${NC} ✅ Agent call complete: ${agent_name} (${duration}ms)"
                ;;
            tool_invocation)
                echo -e "  ${color}[${agent}]${NC} 🔧 Tool invoked: ${tool_name}"
                ;;
            tool_result)
                if [[ "$data" =~ "_mock.*true" ]]; then
                    echo -e "  ${color}[${agent}]${NC}    └─ ${tool_name} result (MOCK)"
                else
                    echo -e "  ${color}[${agent}]${NC}    └─ ${tool_name} result"
                fi
                ;;
            mcp_tool_execution)
                echo -e "  ${color}[${agent}]${NC} 🔌 MCP tool: ${tool_name}"
                ;;
            response_complete)
                echo -e "  ${color}[${agent}]${NC} ✅ Response complete"
                ;;
        esac
    fi
}

# Monitor both agents in parallel
(
    curl -N -s "http://localhost:${PORT1}/events?category=AGENT,TOOL,MCP,CHAT" | while IFS= read -r line; do
        format_event "$LABEL1" "$GREEN" "$line"
    done
) &
PID1=$!

(
    curl -N -s "http://localhost:${PORT2}/events?category=AGENT,TOOL,MCP,CHAT" | while IFS= read -r line; do
        format_event "$LABEL2" "$CYAN" "$line"
    done
) &
PID2=$!

# Cleanup on exit
trap "kill $PID1 $PID2 2>/dev/null" EXIT INT TERM

# Wait for both monitors
wait
