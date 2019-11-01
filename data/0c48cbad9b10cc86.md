---

url: "http://techblog.appnexus.com/2013/2013-09-17-outage-postmortem/"
start_time: ""
end_time: ""
categories:
- postmortem
company: "AppNexus"
product: ""

---

A double free revealed by a database update caused all "impression bus" servers to crash simultaneously. This wasn't caught in staging and made it into production because a time delay is required to trigger the bug, and the staging period didn't have a built-in delay.
