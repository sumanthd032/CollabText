# CollabText

A LAN-first, hybrid real-time collaborative text editor built with Go, JavaScript, and Docker. 
This project demonstrates a robust architecture for building collaborative applications that prioritize 
local network performance and offline capability while seamlessly syncing to a central server when available.

## Core Features
- **LAN-First Collaboration**: Edits are synced directly between peers on the same local network with very low latency.
- **Hybrid Cloud Sync**: Automatically connects to a central server to sync changes with peers on different networks.
- **Conflict-Free Editing**: Powered by Operation-Based CRDTs (Conflict-free Replicated Data Types) to merge concurrent edits without conflicts or data loss.
- **Offline Support**: The local agent persists document state to an embedded database (bbolt), allowing for offline use and ensuring no data is lost.
- **User Presence**: See who is currently online and editing the document in real-time.
- **Simple & Modern Stack**: A minimal dependency Go backend, a vanilla JavaScript frontend, and a containerized infrastructure with Docker Compose.

## Architecture Overview
The system consists of three main components that work together:

1. **Local Agent (Go)**: The core of the application. A lightweight binary that runs on each user's machine. It serves the web UI, discovers local peers, manages the CRDT document model, persists data locally, and syncs with the central server.
2. **Central Sync Server (Go)**: An optional cloud-based component that acts as a relay and durable storage backend. It uses Redis for real-time messaging and PostgreSQL for storing document snapshots and user data.
3. **Client UI (HTML/JS/Tailwind CSS)**: A clean, browser-based single-page application that communicates exclusively with its local agent via WebSockets.

## Tech Stack
- **Backend**: Go, Gorilla WebSocket, Gorilla Mux
- **Frontend**: Vanilla JavaScript, HTML5, Tailwind CSS
- **Database**: PostgreSQL (Cloud), bbolt (Embedded Local)
- **Messaging & Caching**: Redis (Pub/Sub)
- **DevOps & Infrastructure**: Docker, Docker Compose

## Getting Started
You can get the entire application stack—both agents, the server, and the databases—running on your local machine with a single command.

### Prerequisites
- Docker and Docker Compose (essential)
- Git for cloning the repository

### How to Run
**Clone the Repository**
```bash
git clone https://github.com/sumanthd032/collabtext.git
cd collabtext
```

**Build and Run with Docker Compose**
```bash
docker-compose up --build -d
```

After a minute, you can check that all services are running with `docker-compose ps`. 
You should see `collabtext-postgres`, `collabtext-redis`, `collabtext-server`, and `collabtext-agent_1` in the Up state.

**Access the Application**
Open your web browser and navigate to [http://localhost:8080](http://localhost:8080).

You will see the homepage. Click "Create New Document" to start editing.

**Simulate a Second User**
```bash
docker-compose run --service-ports agent_2
```
Now, open a second browser window (or an incognito tab) and navigate to [http://localhost:8082](http://localhost:8082).

Copy the document ID from the URL of the first user (the part after the `#`) and use it to join the same document on the second user's screen.

You should now see both users in the "Online" list and be able to edit text in real-time between the two windows!