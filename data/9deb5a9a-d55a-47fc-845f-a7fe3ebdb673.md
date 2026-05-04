---
uuid: 9deb5a9a-d55a-47fc-845f-a7fe3ebdb673
url: https://status.cloud.google.com/incidents/ow5i3PPK96RduMcb1SsW
title: Global Google Cloud API outage due to Service Control null pointer exception
start_time: 2025-06-12T17:49:00Z
end_time: 2025-06-13T01:18:00Z
categories:
- automation
- cloud
- config-change
- postmortem
keywords:
- service control
- api
- google cloud
- 503 errors
- null pointer
- spanner
- global
- us-central1
company: Google
product: Service Control
source_fetched_at: 2026-05-04T19:52:10.583687Z
summary: A policy change containing blank fields triggered a null pointer exception in Service Control, Google's API management and control plane system. The code path that failed was not feature flag protected and lacked proper error handling. When the policy data replicated globally, it caused Service Control binaries to crash loop across all regions. While a red-button fix was deployed within 40 minutes, larger regions like us-central-1 experienced extended recovery times (up to 2h 40m) due to a thundering herd problem when Service Control tasks restarted, overloading the underlying Spanner infrastructure. The incident affected Google and Google Cloud APIs globally, with recovery times varying by product architecture.

---

On June 12, 2025, Google Cloud, Google Workspace, and Google Security Operations products experienced a global service disruption characterized by increased 503 errors in external API requests. This incident affected a wide array of Google Cloud services, leading to intermittent API and user-interface access issues for customers worldwide. The issue began around 10:49 PDT and impacted multiple regions, with varying recovery times.

The root cause was identified as a null pointer exception within Service Control, Google's API management and control plane system. A new feature added on May 29, 2025, for quota policy checks contained a code path that lacked proper error handling and feature flag protection. On June 12, a policy change with unintended blank fields was inserted into Service Control's regional Spanner tables. This policy data, replicated globally, triggered the vulnerable code path, causing Service Control binaries to enter a crash loop across all regional deployments.

Google's Site Reliability Engineering team began triaging within two minutes, identifying the root cause and initiating a "red-button" fix to disable the offending policy serving path within 10 minutes. The rollout of this fix was completed within 40 minutes, leading to recovery in smaller regions. However, larger regions like us-central-1 experienced extended recovery times, up to 2 hours and 40 minutes, due to a "thundering herd" effect on the underlying Spanner infrastructure as Service Control tasks restarted without appropriate randomized exponential backoff.

The incident led to widespread customer impact, with many experiencing 503 errors and service unavailability. Compounding the issue, the Cloud Service Health infrastructure itself was affected, delaying initial incident reports and leaving some customers without a clear signal of the outage. While most services recovered by 13:45 PDT, some residual impacts, particularly on services like Dataflow and Vertex AI Online Prediction, persisted until 18:18 PDT.

Google has committed to several remediation steps. These include freezing changes to the Service Control stack, modularizing its architecture to fail open, auditing all systems consuming globally replicated data for incremental propagation, enforcing feature flag protection for critical binaries, improving error handling and testing, ensuring randomized exponential backoff in systems, and enhancing external communication channels to remain operational even during widespread outages.

