---
uuid: a54b2ace-5fdf-452e-bdb2-cd8a48a94e56
url: https://engineering.fb.com/2021/10/05/networking-traffic/outage-details/
title: Facebook global backbone network outage of October 2021
categories: []
keywords:
- backbone network
- dns
- bgp
- routers
- data centers
- global
- maintenance
- configuration
company: Facebook
product: global backbone network capacity management system
source_published_at: 2021-10-05T17:26:45Z
source_fetched_at: 2026-05-04T17:51:44.063739Z
summary: Configuration changes to Facebook's backbone routers caused a global outage of all Facebook properties and internal tools.

---

On October 4, 2021, Facebook experienced a global outage affecting all its platforms. This incident was triggered by a system managing the company's global backbone network capacity, which connects all its computing facilities and data centers worldwide.

The root cause was a routine maintenance job where a command, intended to assess backbone capacity, unintentionally took down all connections in the backbone network. A bug in an audit tool failed to prevent this command. This led to a complete disconnection of Facebook's data centers from the internet. A secondary issue arose when DNS servers, unable to communicate with data centers, withdrew their BGP advertisements, making them unreachable to the rest of the internet.

The outage resulted in all Facebook properties becoming inaccessible globally. Internally, the total loss of DNS also broke many critical tools used for investigation and resolution, severely hampering engineers' ability to diagnose and fix the problem remotely.

Remediation involved sending engineers physically to data centers due to the lack of remote access. The high security of these facilities made physical access and modification difficult, prolonging the recovery. Once backbone connectivity was restored, services were brought back online carefully, leveraging experience from "storm drills" to manage the potential surge in traffic and prevent further crashes.

Facebook is conducting an extensive review to learn from this incident and improve system resilience. The company acknowledges the trade-off between enhanced security and slower recovery from rare events like this, and plans to strengthen testing, drills, and overall resilience.

