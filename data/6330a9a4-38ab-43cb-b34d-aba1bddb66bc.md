---
uuid: 6330a9a4-38ab-43cb-b34d-aba1bddb66bc
url: https://status.cloud.google.com/incidents/vLsxuKoRvykNHW3nnhsJ
title: Google Cloud Networking, Storage, and BigQuery reduced capacity for lower priority traffic
start_time: 2022-07-15T02:30:00Z
end_time: 2022-07-15T22:02:00Z
categories:
- automation
- cloud
- config-change
- time
keywords:
- google cloud
- networking
- storage
- bigquery
- latency
- capacity
- us-east1
- us-central1
company: Google
product: Google Cloud Networking, Google Cloud Storage, Google BigQuery
source_fetched_at: 2026-05-04T17:48:53.448956Z
summary: Google Cloud Networking experienced reduced capacity for lower priority traffic such as batch, streaming and transfer operations from 19:30 US/Pacific on Thursday, 14 July 2022, through 15:02 US/Pacific on Friday, 15 July 2022. High-priority user-facing traffic was not affected. This service disruption resulted from an issue encountered during a combination of repair work and a routine network software upgrade rollout. Due to the nature of the disruption and resilience capabilities of Google Cloud products, the impacted regions and individual impact windows varied substantially.

---

Google Cloud Networking experienced reduced capacity for lower priority traffic, such as batch, streaming, and transfer operations, from 19:30 US/Pacific on Thursday, 14 July 2022, through 15:02 US/Pacific on Friday, 15 July 2022. This disruption affected multiple regions, including us-east1, southamerica-west1, us-central1, and us-central2, leading to elevated latency and HTTP 500 errors for Google Cloud Storage and elevated latency for Google BigQuery. High-priority user-facing traffic remained unaffected.

The root cause was identified as an issue with a new control plane configuration rollout. This rollout caused a reduction in capacity for low-priority classified traffic within Google’s internal backbone network, which connects data centers. The reduced network capacity subsequently slowed down mitigation efforts, making it more challenging for engineering teams to safely undo the configuration change.

Customers using Google Cloud Storage experienced elevated latency, delays, issues with importing, exporting, or querying data from GCS buckets, and HTTP 500 errors. Google BigQuery users experienced elevated latency when using the Storage Read and Write APIs. The impact varied across regions and individual impact windows due to the nature of the disruption and the resilience capabilities of Google Cloud products.

Google engineers observed performance degradation around 02:00 US/Pacific on July 15 and began an investigation. Initial mitigation attempts to halt the rollout were made at 03:50 US/Pacific, but ongoing actions continued to reduce capacity. The team then shifted to a global rollback of the problematic configuration, with the first attempt at 08:40 US/Pacific. By 12:40 US/Pacific, the configuration was correctly updated, mitigating the majority of the impact, and services were fully restored by 15:02 US/Pacific.

To prevent future recurrences, Google is implementing improvements in detection, including better signals for traffic changes and enhanced debugging dashboards. Prevention efforts include improved automated handling of disconnected Software Defined Networking (SDN) controllers and global safety systems to halt elective rollouts during capacity reductions. Mitigation strategies involve improving APIs for pushing changes to network controllers during disruptions, enhancing failure domain configuration validation, and investing in testing emergency tools and environments.

