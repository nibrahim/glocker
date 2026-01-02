# Introduction

I needed an application that would block sites which distract me. However, none of the existing solutions I found solve this problem. 

This is almost wholly vibe coded and then manually modified for specific features and to avoid specific problems.

# Challenges
Given the control that Linux offers for `root`, it's hard to make something that *really* block everything. However, it is possible to make it very tedious to break out. That's what this application does. 

I've often found that there are liminal moments where I make the wrong decision in a fog of distraction. Having someone, or if not possible, something that makes it hard to make the wrong decision, lets me get back to work. 

That's what glocker tries to do. 

# Strategies and features

Glocker modifies the `/etc/hosts` file to redirect blocked domains to 127.0.0.1 (localhost). When users attempt to access these blocked domains, glocker detects and tracks these attempts as violations.




# Options
A tool that I've found which does this reasonably well is [plucky](https://getplucky.net/). However, the strategies it employs are not particularly transparent and it's tedious to get it to work. It also has a dependency on a browser and doesn't support firefox which is what I use. I opened a support case and was told that my configuration wouldn't work. Hence, I let that go. 

Another one is [Accountable 2 you](https://accountable2you.com/linux/), but I couldn't get it to work so I let that go too. 

This is my attempt and if it solves my problem, I'll try to make it a hosted service. 
