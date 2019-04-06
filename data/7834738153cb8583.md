---

url: "http://status.mailgun.com/incidents/p9nxxql8g9rh"
start_time: ""
end_time: ""
categories:
- postmortem
company: "Mailgun"
product: ""

---

Secondary MongoDB servers became overloaded and while troubleshooting accidentally pushed a change that sent all secondary traffic to the primary MongoDB server, overloading it as well and exacerbating the problem.
