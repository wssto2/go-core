#!/bin/bash

# Function to kill all background processes on exit
cleanup() {
  echo "Stopping all processes..."
  kill $(jobs -p) 2>/dev/null
  exit 0
}

# Set up trap to call cleanup function on script termination
trap cleanup INT TERM EXIT

# Start Vite in the background
echo "Starting Vite dev server..."
npm run dev --prefix ./frontend &

# Wait for Vite to start
while ! nc -z localhost 5173; do
  sleep 1
done
echo "Vite is running on http://localhost:5173"

# Start the whole cmd/api package so sibling files like app_state.go are included.
echo "Starting Go server with nodemon..."
nodemon --watch './**/*.go' --signal SIGTERM --exec "go run ./cmd/api" &

# Print instructions
echo "Both servers are running!"
echo "Press Ctrl+C to stop both servers."

# Wait for all background processes to finish
# (This will keep the script running until manually terminated)
wait
