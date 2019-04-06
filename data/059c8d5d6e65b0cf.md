---

url: "http://www.bbc.co.uk/blogs/internet/entries/a37b0470-47d4-3991-82bb-a7d5b8803771"
start_time: ""
end_time: ""
categories:
- postmortem
company: "BBC Online"
product: ""

---

In July 2014, BBC Online experienced a very long outage of several of its popular online services including the BBC iPlayer. When the database backend was overloaded, it had started to throttle requests from various services. Services that hadn't cached the database responses locally began timing out and eventually failed completely.
