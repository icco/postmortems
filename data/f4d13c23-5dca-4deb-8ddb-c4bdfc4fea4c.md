---
uuid: f4d13c23-5dca-4deb-8ddb-c4bdfc4fea4c
url: https://web.archive.org/web/20220427012208/https://lkml.org/lkml/2009/1/2/373
title: Linux kernel leap second deadlock crash on New Year's 2008-2009
start_time: 2009-01-01T00:00:00Z
categories:
- cloud
- postmortem
- time
keywords:
- linux
- kernel
- leap second
- deadlock
- printk
- xtime_lock
- ntp
- crash
company: Linux
product: linux kernel
source_fetched_at: 2026-05-04T17:55:22.496821Z
summary: Leap second code was called from the timer interrupt handler, which held `xtime_lock`. That code did a `printk` to log the leap second. `printk` wakes up `klogd`, which can sometimes try to get the time, which waits on `xtime_lock`, causing a deadlock.

---

On New Year's 2008-2009, Linux systems experienced crashes related to the introduction of a leap second. The issue was observed on kernels such as `kernel-2.6.26.6-49.fc8.x86_64` and `RHEL 4 kernel 2.6.9-67.0.7.EL`.

The root cause was identified as a deadlock condition. The leap second code, specifically `ntp_leap_second`, was invoked from the timer interrupt handler while holding the `xtime_lock`. Within this critical section, the code performed a `printk` operation to log the leap second event.

The `printk` function, in turn, attempts to wake up `klogd`. Under conditions where the system is busy, the scheduler, during the `klogd` wake-up process, may try to obtain the current time. This action requires acquiring the `xtime_lock`, which was already held by the timer interrupt handler, leading to a deadlock and system crash.

Customer impact included system instability and crashes, particularly on busy systems. The problem was reproducible by triggering the leap second event on a loaded system. An immediate, albeit crude, fix suggested was to prevent `printk` calls while `xtime_lock` is held within the NTP code. A more robust solution would aim to provide leap second notifications without introducing such deadlocks.

