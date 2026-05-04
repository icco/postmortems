---
uuid: ef3cc3e0-c51c-4f5d-8dd0-cc8a8dfdcbb2
url: https://dougseven.com/2014/04/17/knightmare-a-devops-cautionary-tale/
title: Knight Capital SMARS algorithmic trading incident of August 2012
start_time: 2012-08-01T13:30:00Z
end_time: 2012-08-01T14:15:00Z
categories:
- automation
- cloud
- config-change
- postmortem
keywords:
- knight capital
- smars
- power peg
- algorithmic trading
- deployment
- manual error
- financial
- market making
company: Knight Capital
product: SMARS
source_published_at: 2014-04-17T20:05:00Z
source_fetched_at: 2026-05-04T17:55:08.203607Z
summary: A combination of conflicting deployed versions and re-using a previously used bit caused a $460M loss. See also a [longer write-up](https://www.henricodolfing.com/2019/06/project-failure-case-study-knight-capital.html).

---

On August 1, 2012, Knight Capital Group experienced a catastrophic failure in its automated trading system, SMARS (Supplemental Market Access Routing System), shortly after the market opened at 9:30 AM ET. The incident, which lasted 45 minutes, led to a $460 million loss for the firm. This occurred during the launch of the NYSE's new Retail Liquidity Program, for which Knight had updated its SMARS system.

The root cause was a manual deployment error between July 27 and July 31, 2012. While updating SMARS, a technician failed to copy the new code to one of eight servers. This left an old, unused "Power Peg" functionality, dormant for eight years, active on that single server. The new code repurposed a flag to activate the Retail Liquidity Program functionality, but on the un-updated server, this flag instead reactivated the old Power Peg code.

The reactivated Power Peg code, originally designed to count shares against a parent order and stop routing child orders once fulfilled, had its cumulative tracking functionality moved to an earlier stage in 2005. When triggered on the eighth server, it began routing child orders without tracking the parent order fulfillment, effectively creating an endless loop of order generation. This resulted in millions of erroneous child orders flooding the market.

Within 45 minutes, Knight's executions constituted over 50% of the trading volume, driving certain stocks up over 10% and causing others to decrease. The system processed 212 parent orders, resulting in 4 million transactions against 154 stocks for over 397 million shares. Knight Capital Group incurred a net loss of $460 million, exceeding its $365 million in cash and equivalents, effectively rendering the company bankrupt.

Initial attempts to stop the erroneous trades were unsuccessful, partly due to the absence of a "kill switch" and documented procedures. Knight's personnel mistakenly uninstalled the *working* new code from the seven correctly updated servers, amplifying the problem. The system was eventually stopped after 45 minutes. The incident highlighted the critical importance of automated, repeatable deployment processes and the need for robust emergency stop mechanisms to prevent human error and mitigate risk in high-stakes environments.

