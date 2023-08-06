---
uuid: f4d13c23-5dca-4deb-8ddb-c4bdfc4fea4c
url: https://web.archive.org/web/20220427012208/https://lkml.org/lkml/2009/1/2/373
categories:
- postmortem
company: Linux
product: ""

---

Leap second code was called from the timer interrupt handler, which held `xtime_lock`. That code did a `printk` to log the leap second. `printk` wakes up `klogd`, which can sometimes try to get the time, which waits on `xtime_lock`, causing a deadlock.
