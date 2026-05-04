---
uuid: f4d13c23-5dca-4deb-8ddb-c4bdfc4fea4c
url: https://lkml.org/lkml/2009/1/2/373
archive_url: https://web.archive.org/web/20220427012208if_/https://lkml.org/lkml/2009/1/2/373
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
source_fetched_at: 2026-05-04T18:20:19.155321Z
summary: Leap second code was called from the timer interrupt handler, which held `xtime_lock`. That code did a `printk` to log the leap second. `printk` wakes up `klogd`, which can sometimes try to get the time, which waits on `xtime_lock`, causing a deadlock.

---

On New Year's 2008-2009, Linux systems experienced crashes related to the introduction of a leap second. This issue was observed on various kernel versions, including `kernel-2.6.26.6-49.fc8.x86_64` on Fedora 8 and `RHEL 4 kernel 2.6.9-67.0.7.EL`. The crashes were particularly prevalent on busy systems.

The root cause was identified as a deadlock condition within the kernel's timekeeping and logging mechanisms. Specifically, the `ntp_leap_second` function, responsible for handling the leap second event, was invoked from the timer interrupt handler. At this point, the `xtime_lock` was already held by the interrupt handler.

Within this critical section, the `ntp_leap_second` code performed a `printk` operation to log the leap second event. The `printk` function, in turn, attempted to wake up `klogd`. Under conditions where the system was busy, the scheduler, during the `klogd` wake-up process, tried to obtain the current time. This action required acquiring the `xtime_lock` again, which was already held, leading to a recursive lock attempt and a system-wide deadlock.

The primary customer impact was system instability and crashes. Users experienced system failures, especially on machines under load, as the deadlock was more likely to occur when the scheduler was active and contending for resources. The problem was reproducible by triggering the leap second event on a loaded system.

An immediate, albeit temporary, fix suggested was to prevent `printk` calls while `xtime_lock` is held within the NTP code. A more robust and desirable solution would aim to provide leap second notifications without introducing such deadlocks, ensuring system stability during these time adjustments.

