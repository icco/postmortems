---
uuid: dc34b433-0ddd-45dd-85bf-cfa6d8cce764
url: https://status.circleci.com/incidents/8rklh3qqckp1
title: CircleCI Linux build queue backing up October 2015
start_time: 2015-10-14T20:17:00Z
end_time: 2015-10-15T14:20:00Z
categories:
- config-change
- postmortem
keywords:
- linux
- build queue
- database
- ci/cd
- throttling
- backlog
- circleci
company: CircleCI
product: linux build queue
source_fetched_at: 2026-05-04T19:52:39.692015Z
summary: At peak Wednesday-afternoon load, the primary database backed up with queued operations to the point that it stopped catching up; rolling back recent changes had no isolated effect because the queue depth had already saturated the system, and a primary failover to kill queued ops only bought temporary headroom. After the runnable-queue was drained, builds were stuck in the prior queue stage; manually promoting them flooded the next queue, and the build-scheduler's failure-mode throttles fired on what were actually normal conditions and backed off precisely when more throughput was needed. CircleCI rebuilt tooling on the fly to clear a 17-hour backlog.

---

On Wednesday, October 14, 2015, starting around 20:17 UTC, CircleCI experienced a significant incident where the Linux build queue began backing up. The operations team observed that standard demand management tools were ineffective, and capacity was available but not being utilized for builds. This led to a rising queue and an escalation to engineering.

Initial investigations focused on increased database load, which was exacerbated during peak Wednesday afternoon traffic. While no direct correlation to recent changes was found, suspicious changes were rolled back. However, the database was already saturated with queued operations. A failover to a different primary database at 00:11 UTC on Thursday provided temporary relief, but queued operations quickly returned.

By Thursday 07:00 UTC, runnable builds were processing, but a large number of builds were blocked from reaching the runnable state. Attempts to promote these builds using normal code flooded the next queue. Furthermore, the build scheduler's throttling mechanisms, designed to back off during failure, were misfiring under normal conditions, preventing necessary throughput. This resulted in a 17-hour backlog of builds.

CircleCI engineers manually forced builds through and rapidly developed new tools to automate batch processing of the backlog. They also added new metrics and updated the throttling code to improve behavior. By Thursday 14:20 UTC, the last of the leftover builds were processed, and the system returned to handling new inbound traffic normally.

The incident highlighted a lack of immediate tools to manage such situations, leading to on-the-fly development and inconsistent build states. CircleCI committed to investing in better tools for rapid incident response. Architecturally, ongoing efforts to reduce database strain through a central scheduler, data migration to separate databases, and custom-tuned deployments for different data types were reinforced as critical for future reliability.

