---

url: "http://www.faqs.org/rfcs/rfc789.html"
start_time: ""
end_time: ""
categories:
- postmortem
company: "ARPANET"
product: ""

---

A malfunctioning IMP ([Interface Message Processor](https://en.wikipedia.org/wiki/Interface_Message_Processor)) corrupted routing data, software recomputed checksums propagating bad data with good checksums, incorrect sequence numbers caused buffers to fill, full buffers caused loss of keepalive packets and nodes took themselves off the network. From 1980.
