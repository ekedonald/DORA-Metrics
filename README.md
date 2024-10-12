# Deploying DORA Metrics App with Docker

## Table of Contents
- [Introduction to DORA Metrics](#introduction-to-dora-metrics)
- [Metrics Exposed by the App](#metrics-exposed-by-the-app)
- [Deployment Guide](#deployment-guide)
  - [Prerequisites](#prerequisites)
  - [Step 1: Generate Webhook Secret](#step-1-generate-webhook-secret)
  - [Step 2: Create .env File](#step-2-create-env-file)
  - [Step 3: Create Dockerfile](#step-3-create-dockerfile)
  - [Step 4: Build and Run Docker Container](#step-4-build-and-run-docker-container)
  - [Step 5: Set Up GitHub Webhook](#step-5-set-up-github-webhook)
  - [Step 6: Integrate with Prometheus](#step-6-integrate-with-prometheus)
- [Using the DORA Metrics App](#using-the-dora-metrics-app)


## Introduction to DORA Metrics

DORA (DevOps Research and Assessment) metrics are key performance indicators used to measure the effectiveness of software development and delivery processes. These metrics established by the DORA research program, help organizations assess and improve their DevOps practices. The four key DORA metrics are:

1. **Deployment Frequency**: How often an organization successfully releases to production.
2. **Lead Time for Changes**: The time it takes from code commit to code running in production.
3. **Time to Restore Service**: How long it takes to recover from a failure in production.
4. **Change Failure Rate**: The percentage of deployments causing a failure in production.

## Metrics Exposed by the App

This DORA metrics app exposes the following Prometheus metrics:

- `dora_deployment_frequency`: Deployment Frequency metric.
- `dora_lead_time_for_changes_minutes`: Lead Time for Changes metric (in minutes).
- `dora_time_to_restore_service`: Time to Restore Service metric.
- `dora_change_failure_rate`: Change Failure Rate metric.
- `dora_successful_deployments`: Number of successful deployments in the last 30 days.
- `dora_failed_deployments`: Number of failed deployments in the last 30 days.

All metrics are labeled with the `branch` they correspond to.

## Deployment Guide

### Prerequisites

- [Docker](https://docs.docker.com/engine/install/) installed on your system
- Git repository with GitHub Actions workflows set up
- GitHub Personal Access Token with appropriate permissions

### Step 1: Generate Webhook Secret

Generate a secure webhook secret using OpenSSL:

```bash
openssl rand -hex 20
```

_Save this generated secret; you'll need it later._

### Step 2: Create .env File

Create a `.env` file in the project root with the following content:

```
GITHUB_TOKEN=<your-github-personal-access-token>
WEBHOOK_SECRET=<generated-webhook-secret>
```

Replace `<your-github-personal-access-token>` with your GitHub Personal Access Token and `<generated-webhook-secret>` with the secret you generated in Step 1.

### Step 3: Create Dockerfile

Create a `Dockerfile` in the project root with the following content:

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o dora-metrics .

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/dora-metrics .
EXPOSE 4040
CMD ["./dora-metrics"]
```

### Step 4: Build and Run Docker Container

Build the Docker image:

```bash
docker build -t dora-metrics-app .
```

Run the Docker container:

```bash
docker run -d --name dora-metrics -p 4040:4040 --env-file .env dora-metrics-app
```

This command runs the container in detached mode, maps port 4040 from the container to the host and uses the environment variables from the `.env` file.

### Step 5: Set Up GitHub Webhook

1. Go to your GitHub repository settings.
2. Navigate to "Webhooks" and click "Add webhook".
3. Set the Payload URL to `http://<your-server-ip>:4040/webhook`.
4. Set the Content type to `application/json`.
5. Enter the webhook secret you generated in Step 1.
6. Select the events you want to trigger the webhook (e.g. Pushes, Workflow runs).
7. Click "Add webhook".

### Step 6: Integrate with Prometheus

To scrape metrics from your DORA metrics app, add the following job to your Prometheus configuration:

```yaml
scrape_configs:
  - job_name: 'dora_metrics'
    static_configs:
      - targets: ['<your-server-ip>:4040']
```

Replace `<your-server-ip>` with the IP address or hostname of the server running your Docker container.

## Using the DORA Metrics App

Once deployed, the app will start collecting DORA metrics based on your GitHub repository's activity. You can access the raw metrics by visiting `http://<your-server-ip>:4040/metrics`.

The app responds to GitHub webhook events to update metrics in real-time. It calculates:

- **Deployment Frequency** based on successful workflow runs.
- **Lead Time for Changes** by analyzing the time between commit and successful deployment.
- **Time to Restore Service** by examining issues labeled as "incident".
- **Change Failure Rate** by comparing failed deployments to total deployments.

You can visualize these metrics using Grafana or any other Prometheus-compatible visualization tool.

By following this guide, you'll have a functioning DORA metrics app deployed using Docker, integrated with your GitHub repository and ready to be scraped by Prometheus for visualization and analysis.