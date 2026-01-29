#!/bin/bash

# Parse and display combined event timeline from agent logs

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
WHITE='\033[1;37m'
NC='\033[0m'

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
LOG_A="$SCRIPT_DIR/logs/agent-a.log"
LOG_B="$SCRIPT_DIR/logs/agent-b.log"

echo -e "${BLUE}╔══════════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║        Agent-to-Agent Communication Event Timeline                   ║${NC}"
echo -e "${BLUE}╚══════════════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Function to extract events from logs
extract_events() {
    local log_file=$1
    local agent_name=$2
    local color=$3

    # Extract key events with timestamps
    grep -E "POST.*chat|📝 Adding tool_result|🔵 Iteration|✅ Conversation complete|Calling agent|agent_id=|Tool use|list_available_agents|call_agent" "$log_file" | while IFS= read -r line; do
        # Extract timestamp
        timestamp=$(echo "$line" | grep -oE '^[0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}')

        # Extract time only (HH:MM:SS)
        time_only=$(echo "$timestamp" | cut -d' ' -f2)

        # Determine event type and format
        if [[ "$line" =~ "POST" ]] && [[ "$line" =~ "chat" ]]; then
            # Extract message snippet
            msg=$(echo "$line" | grep -oP 'Body: \K.*' | cut -c1-60)
            echo "${time_only}|${agent_name}|${color}📨 Received request: ${msg}...${NC}"

        elif [[ "$line" =~ "📝 Adding tool_result: tool=call_agent" ]]; then
            # Extract agent call result
            if [[ "$line" =~ "error" ]]; then
                error=$(echo "$line" | grep -oP 'error":"[^"]+' | cut -d'"' -f3)
                echo "${time_only}|${agent_name}|${color}❌ call_agent failed: ${error}${NC}"
            else
                duration=$(echo "$line" | grep -oP 'duration_ms":[0-9]+' | cut -d':' -f2)
                echo "${time_only}|${agent_name}|${color}✅ call_agent succeeded (${duration}ms)${NC}"
            fi

        elif [[ "$line" =~ "📝 Adding tool_result: tool=list_available_agents" ]]; then
            count=$(echo "$line" | grep -oP '"count":[0-9]+' | cut -d':' -f2)
            echo "${time_only}|${agent_name}|${color}📋 list_available_agents returned ${count} agent(s)${NC}"

        elif [[ "$line" =~ "🔵 Iteration" ]]; then
            iter=$(echo "$line" | grep -oP 'Iteration \K[0-9]+')
            tools=$(echo "$line" | grep -oP '\K[0-9]+ tool result')
            echo "${time_only}|${agent_name}|${color}🔄 LLM iteration ${iter} complete (${tools}s)${NC}"

        elif [[ "$line" =~ "✅ Conversation complete" ]]; then
            echo "${time_only}|${agent_name}|${color}✅ Response complete${NC}"
        fi
    done
}

echo -e "${WHITE}Analyzing logs...${NC}"
echo ""

# Create temporary files with parsed events
extract_events "$LOG_A" "Agent-A" "$GREEN" > /tmp/events-a.txt
extract_events "$LOG_B" "Agent-B" "$MAGENTA" > /tmp/events-b.txt

# Merge and sort by timestamp
cat /tmp/events-a.txt /tmp/events-b.txt | sort | while IFS='|' read -r time agent event; do
    printf "${CYAN}[%s]${NC} ${YELLOW}%-12s${NC} %b\n" "$time" "$agent" "$event"
done

echo ""
echo -e "${BLUE}════════════════════════════════════════════════════════════${NC}"
echo -e "${WHITE}Key Observations:${NC}"
echo ""
echo -e "  ${GREEN}Agent A${NC} receives user request"
echo -e "  ${GREEN}Agent A${NC} tries call_agent (fails - wrong ID)"
echo -e "  ${GREEN}Agent A${NC} uses list_available_agents (discovers correct ID)"
echo -e "  ${GREEN}Agent A${NC} retries call_agent with correct ID"
echo -e "  ${MAGENTA}Agent B${NC} receives request from Agent A"
echo -e "  ${MAGENTA}Agent B${NC} generates code response"
echo -e "  ${GREEN}Agent A${NC} receives response from Agent B"
echo -e "  ${GREEN}Agent A${NC} formats final response for user"
echo ""

# Cleanup
rm -f /tmp/events-a.txt /tmp/events-b.txt
