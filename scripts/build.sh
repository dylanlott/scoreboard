#!/bin/bash

if [ -f "docker build -t scoreboard ." ]; then
    # Check if the command was successful
    if [ $? -eq 0 ]; then
        echo "Scoreboard built successfully"
    else
        echo "Failed to build Scoreboard"
    fi
else
    echo "Failed to build Docker image"
fi