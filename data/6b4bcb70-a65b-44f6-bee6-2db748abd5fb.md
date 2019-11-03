---

uuid: "6b4bcb70-a65b-44f6-bee6-2db748abd5fb"
url: "http://stackstatus.net/post/147710624694/outage-postmortem-july-20-2016"
start_time: ""
end_time: ""
categories:
- postmortem
company: "Stack Exchange"
product: ""

---

Backtracking implementation in the underlying regex engine turned out to be very expensive for a particular post leading to health-check failures and eventual outage.
