#!/bin/bash

# Check if the binary exists
if [ -f "docker run -p 8080:8080 scoreboard" ]; then
    # Check if the command was successful
    if [ $? -eq 0 ]; then
        echo "Scoreboard started successfully"
    else
        echo "Failed to start Scoreboard"
    fi
else
    echo "Failed to run Docker image"
fi
