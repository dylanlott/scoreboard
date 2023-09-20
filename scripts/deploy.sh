#!/bin/bash

LOCAL_BINARY="./"
REMOTE_USER="root"
REMOTE_HOST="www.dylanlott.com"
REMOTE_DIR="/mnt/volume_sfo3_01/scoreboard"
DOCKER_COMPOSE_FILE="docker-compose.yml"
DOCKER_COMPOSE_COMMAND="up -d"

# Rsync command to copy local directory to remote server
rsync -avz $LOCAL_BINARY $REMOTE_USER@$REMOTE_HOST:$REMOTE_DIR

# Check if rsync command was successful
if [ $? -eq 0 ]; then
    echo "Directory successfully copied to remote server."

    # SSH into the remote server and execute the Docker Compose command.
    ssh $REMOTE_USER@$REMOTE_HOST "cd "$REMOTE_DIR" && ls -al && which docker-compose && docker-compose up -d"
else
    echo "Failed to copy directory to remote server."
fi
