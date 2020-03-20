---
uuid: 7e14860a-fced-4a66-9fdf-40f702ceeccd
url: https://status.heroku.com/incidents/642?postmortem
categories:
- postmortem
company: Heroku
product: ""

---

Having a system that requires scheduled manual updates resulted in an error which caused US customers to be unable to scale, stop or restart dynos, or route HTTP traffic, and also prevented all customers from being able to deploy.
