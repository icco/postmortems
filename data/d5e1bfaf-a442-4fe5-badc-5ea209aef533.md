---
uuid: d5e1bfaf-a442-4fe5-badc-5ea209aef533
url: https://aws.amazon.com/message/656481/
title: AWS SA-EAST-1 Availability Zone Power and Network Incident, December 2013
start_time: 2013-12-18T06:05:00Z
categories:
- automation
- cloud
- config-change
- hardware
keywords:
- aws
- sa-east-1
- power
- generator
- network
- outage
- brazil
- availability zone
company: Amazon
product: ""
source_fetched_at: 2026-05-04T19:52:37.145417Z
summary: Utility power was lost at a São Paulo AZ; during failover a breaker opened in front of one generator and a second generator independently failed mechanically, leaving the remaining healthy generators overloaded so they also shut down. The site's automated power-control system then malfunctioned, forcing operators to bring generators online manually. After power was restored, a network technician brought a device back up with a bad config that advertised an invalid route, degrading internet connectivity for both AZs in SA-EAST-1 for ~20 minutes.

---

On December 17th, 2013, at 10:05 PM PST, a single Availability Zone in the AWS South America Region (SA-EAST-1) experienced a loss of utility power due to a fault at a local substation. The facility's backup generators engaged as designed, but during this failover, a breaker in front of one generator opened, and a second generator independently failed mechanically. This left the remaining healthy generators overloaded, causing them to shut down as well.

The facility's automated power-control system then malfunctioned, preventing the quick restoration of power. Operators had to bypass the automated system and manually bring generators online, a slow process. Once sufficient generator capacity was restored, impacted instances began to recover.

During the recovery process, a network technician manually brought a network device back online in the power-impacted Availability Zone with a bad configuration. This misconfiguration caused the device to advertise an invalid network route, leading to degraded internet connectivity for both Availability Zones in SA-EAST-1 for approximately 20 minutes.

The root causes included the initial utility power loss, a double failure of backup generators, a malfunction in the automated power control system, and a human error introducing a bad network configuration during recovery. The double generator failure was noted as extremely unusual.

Customer impact involved service disruptions for instances in the affected Availability Zone due to power loss, followed by a 20-minute period of degraded internet connectivity for both SA-EAST-1 Availability Zones. Remediation involved manual power restoration and the removal of the misconfigured network device.

AWS committed to reviewing operational records of failed components and taking steps to improve the Availability Zone's resilience to similar power failures.

