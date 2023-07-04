#!/bin/bash

# Define local directory and remote server details
LOCAL_BINARY="./"
REMOTE_USER="root"
REMOTE_HOST="www.dylanlott.com"
REMOTE_DIR="/mnt/volume_sfo3_01/scoreboard"

# Rsync command to copy local directory to remote server
rsync -avz $LOCAL_BINARY $REMOTE_USER@$REMOTE_HOST:$REMOTE_DIR

# Check if rsync command was successful
if [ $? -eq 0 ]; then
    echo "Directory successfully copied to remote server."
else
    echo "Failed to copy directory to remote server."
fi
