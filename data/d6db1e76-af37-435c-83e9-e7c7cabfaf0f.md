---
uuid: d6db1e76-af37-435c-83e9-e7c7cabfaf0f
url: https://medium.com/square-corner-blog/incident-summary-2017-03-16-2f65be39297
archive_url: https://web.archive.org/web/20210818034431if_/https://medium.com/square-corner-blog/incident-summary-2017-03-16-2f65be39297
title: Square service disruption of March 16, 2017
start_time: 2017-03-16T17:02:00Z
end_time: 2017-03-16T20:12:00Z
categories:
- cloud
- config-change
- postmortem
- security
keywords:
- square
- multipass
- redis
- authentication
- payment processing
- outage
- 2fa
- sms
company: Square
product: Multipass
source_published_at: 2019-04-18T22:40:36.082Z
source_fetched_at: 2026-05-04T17:53:56.418725Z
summary: A cascading error from an adjacent service lead to merchant authentication service being overloaded. This impacted merchants for ~2 hours.

---

On March 16, 2017, Square's serving infrastructure experienced a widespread service disruption, primarily affecting the "Multipass" customer authentication service. This incident impacted most of Square's products and services, including payment processing, Point of Sale, Dashboard, Appointments, and Payroll.

The root cause was identified as the Multipass service becoming unrecoverably overloaded. This was triggered by a deployment of the "Roster" service (which handles customer identity) in a single datacenter. A critical contributing factor was a harmful feedback loop within Multipass's Redis database interactions, where a specific code path had a high upper bound (500) for retries on optimistic transactions with no backoff, causing Redis to peak on capacity.

Customers experienced a service outage for all merchants between 10:02 a.m. and 11:55 a.m. PDT. Additionally, two-factor authentication (2FA) via SMS was disrupted for a longer period, until 1:12 p.m. PDT.

Initial remediation attempts, including rolling back Roster changes and restarting Multipass, were unsuccessful. Engineers then diagnosed the Redis retry issue in Multipass, developed a fix to reduce retries, and deployed it, which restored Multipass and payment flows. The subsequent 2FA SMS issue was resolved by contacting the SMS vendor and rebalancing outbound SMS phone numbers to increase capacity.
