#!/bin/bash

# Simple restart wrapper for testing
BINARY="/usr/local/bin/cctv-agent"
CONFIG="$HOME/.cctv-agent/config.json"
LOG_DIR="$HOME/.cctv-agent/logs"

echo "Starting CCTV Agent with auto-restart..."
echo "Logs: $LOG_DIR/restart.log"
echo "Press Ctrl+C to stop"

while true; do
    echo "$(date): Starting CCTV Agent..." >> "$LOG_DIR/restart.log"
    $BINARY --config $CONFIG >> "$LOG_DIR/output.log" 2>&1
    EXIT_CODE=$?
    echo "$(date): CCTV Agent exited with code $EXIT_CODE" >> "$LOG_DIR/restart.log"
    
    if [ $EXIT_CODE -eq 0 ]; then
        echo "$(date): Clean exit, stopping restart loop" >> "$LOG_DIR/restart.log"
        break
    fi
    
    echo "$(date): Restarting in 5 seconds..." >> "$LOG_DIR/restart.log"
    sleep 5
done