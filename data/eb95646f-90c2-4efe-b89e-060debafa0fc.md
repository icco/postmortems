---
uuid: eb95646f-90c2-4efe-b89e-060debafa0fc
url: https://incident.io/blog/clouds-caches-and-connection-conundrums
title: incident.io GKE Dataplane V2 `anetd` CPU saturation causes connection timeouts
categories:
- cloud
- config-change
keywords:
- google cloud
- gke
- kubernetes
- postgres
- memcached
- dataplane v2
- anetd
- networking
company: incident.io
product: GKE Dataplane V2
source_published_at: 2023-09-26T08:13:00Z
source_fetched_at: 2026-05-04T19:52:47.751035Z
summary: "After moving to Google Cloud they saw spikes of Postgres connection timeouts (~200 new connections/s) and memcached \"i/o timeout\" errors. Pool tuning (15m→30m max lifetime, static pool size, `MaxIdleConns` 2→20) helped each in turn but didn't eliminate the cache errors. The smoking gun was GKE Dataplane V2: bursts of parallel outbound calls (made worse by an accidental N+1 join) saturated the per-node `anetd` agent's CPU, dropping packets between the node and other services running on it."

---

Following a migration to Google Cloud, incident.io experienced a spike in Postgres connection timeouts, with hundreds of new connections being opened per second. Initial investigations led to tuning the Postgres connection pool by doubling the maximum connection lifespan to 30 minutes and making the pools static, which significantly reduced new connection creation.

Subsequently, memcached also began exhibiting sporadic connection timeout issues, evolving into "read i/o" timeouts. Increasing the `MaxIdleConns` from 2 to 20 provided some relief, and a temporary move from Google's hosted memcached to a self-hosted instance within their Kubernetes cluster was attempted, though it did not fully resolve the problem.

A separate issue involving an accidental N+1 database join was discovered, causing thousands of duplicate network calls to an external third party. Fixing this runaway query dramatically reduced overall network activity and temporarily alleviated many of the connection and cache issues, suggesting that the underlying problem was related to high parallel network calls.

The persistent cache errors were eventually traced to GKE Dataplane V2. Bursts of parallel outbound calls, particularly when generating timeline events with many attachments, saturated the CPU of the `anetd` agent on Kubernetes nodes. This CPU exhaustion led to dropped packets and network communication failures between the node and services like memcached running on it.

The incident underscored the importance of considering node-wide activity when diagnosing network timeouts and the necessity of carefully limiting outbound network concurrency. While turning off Dataplane V2 was considered, the team opted to address the root cause by improving connection pooling and concurrency management, rather than disabling a core GKE feature.

