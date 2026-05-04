---
uuid: cf16d8bc-b4cd-469d-94e4-e39fcec5aec9
url: https://web.archive.org/web/20160426163728/https://medium.com/medium-eng/the-curious-case-of-disappearing-polish-s-fa398313d4df
title: Medium editor bug preventing Polish 'Ś' character input
categories:
- postmortem
keywords:
- polish
- keyboard
- ś
- ctrl+s
- alt+s
- windows
- medium
- internationalization
- bug
company: Medium
product: Medium
source_published_at: 2015-02-02T20:09:43.204Z
source_fetched_at: 2026-05-04T17:53:38.972411Z
summary: Polish users were unable to use their "Ś" key on Medium.

---

In early 2015, Medium users in Poland reported an inability to type the character 'Ś' within the Medium editor. This issue was specific to Medium, as the character could be typed elsewhere, and was reported "a few weeks" before the article's publication on February 2, 2015.

The core problem was that when Polish users attempted to type 'Ś' using their standard programmer's keyboard layout, which typically involves pressing Right Alt + S, the character would not appear. This significantly hampered their ability to write in Polish on the platform.

The root cause was a complex interaction of several historical and technical factors. In Poland, due to the lack of customized keyboards in early computing, a "programmer's layout" emerged where diacritics like 'Ś' were typed using Alt key combinations (e.g., Alt+S). On Microsoft Windows, the Right Alt key was internally mapped as a combination of Ctrl + Alt.

Consequently, typing Right Alt + S for 'Ś' was interpreted by the system as Ctrl + Alt + S. Medium's editor, however, had a specific code snippet designed to block Ctrl + S (and by extension, Ctrl + Alt + S) to prevent the browser's default "Save Page As" dialog from appearing, which was considered an annoying user experience. This unintended interception of Ctrl + Alt + S prevented the 'Ś' character from being input.

The remediation involved a simple code change in Medium's editor. The fix was implemented "last week" (late January 2015) and involved modifying the logic to only block Ctrl + S if the Alt key was *not* pressed. This allowed the Ctrl + Alt + S combination, originating from Right Alt + S, to pass through and correctly produce the 'Ś' character.

