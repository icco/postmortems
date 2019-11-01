---

url: "https://www.joyent.com/blog/manta-postmortem-7-27-2015"
start_time: ""
end_time: ""
categories:
- postmortem
company: "Joyent"
product: ""

---

Operations on Manta were blocked because a lock couldn't be obtained on their PostgreSQL metadata servers. This was due to a combination of PostgreSQL's transaction wraparound maintenance taking a lock on something, and a Joyent query that unnecessarily tried to take a global lock.
