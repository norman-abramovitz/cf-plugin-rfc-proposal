# CF CLI Plugins — Current Status and Future

**Speakers:** Norman Abramovitz & Al Berez
**Event:** Cloud Foundry Summit 2025
**Video:** [YouTube](https://www.youtube.com/watch?v=MyYxHkeHvKo)
**Transcript:** Auto-generated captions, lightly cleaned

---

Hello friends. Uh we want to talk a little bit about uh CFCLI and plugins
specifically. We want to touch uh couple um aspects. So maybe like you just uh
get your phones ready to uh capture a couple QR codes and we'll start like by
introducing Norm. Welcome. Welcome. I'm Norm Brahmutz. I've been involved with 
Cloud Foundry now since uh 2015. What's interesting is I've been involved in 
the hardware software industry um um for over 50 years. I touched my first IBM 
360 bra box. And so what I did was for my background, if you if you could tell 
or not, is I researched on Google to find out what could be what could be found 
about me. And and so one of the things I discovered was I was a a visionary 
because I was involved in TCP IP design when it came out with TCP3 and TCP4. 
Okay. I also was involved with um a little company called RSI which was Oracle. 
I remember having arguments with Larry Ellison uh about doing column based 
storage versus row based storage. So there's a lot I'm visionary there's early 
on and the main thing is I like to work with knowledgeable people and that 
that's what I find about cloud foundry tons and tons of knowledgeable people 
here. So thank you. Thank you Norm. Um I'm Alz um I'm obsessed with developers 
uh automation and uh productivity. I've been a Cloud Foundry core contributor 
for the last six years. very privileged to work with very talented people. Um, 
now I'm looking for my next uh chapter and role. Um, please feel free to take a 
snapshot of this uh QR code. Um, and uh the next uh slide we'll talk a bit 
about our objectives and goals. U as many know that uh our Capy V2 is 
deprecated. Yay. as of December uh 24. Um end of life is planned uh around the 
end of 26 which is next year and a lot uh conversation happen in RFC uh 32. Our 
goal is to develop a road map uh for the next generation of CLI plugins uh 
because uh currently uh there is no clear alignment uh what to do and we need 
decisions and we need your help. Please take a snapshot of this QR code. This 
QR code it's a document Google document. It's a pre-RFC work in progress 
document where we would like uh any uh stakeholders to uh participate and uh 
share like your needs and your ideas about um about plug-in system. Um um so 
what I wanted to do is make sure we we put in front of us the end goal because 
everyone all engineers want to know what what we're working towards before we 
get into the story how we got there. So basically we need to make RFC's for the 
TOC. uh we need more help in terms of getting people to uh do they any coding 
and we like to get this all done before the Q1 uh 2026. So it's a a big project 
that we're talking about here. Um my exper Okay. My experience has been with 
with the plugins was I started about two years ago and trying to figure out how 
to use the system and you know you look over you you look over other plugins 
and what's going on. And one of the things I discovered was um what's this um 
code.clawfoundry.org or they they built a redirection within the in the coding 
which caused some issues because when that redirection went down everything 
broke, nothing worked. And then the other thing was I discovered as I was 
looking at it was is that the the the code to use for plugins was frozen in 
time. If you you could tell that this is frozen from September of 2020. It's 
still frozen to September 2020. There's nothing been new added to it. So, we 
need to figure out what we want to do with plugins overall. And because we 
waited so long, what do we have? We have code being archived, which means we 
have code within our CF command right now that is archived away. So, do we 
really need that? We need to figure out how to improve the situation. And L, 
you talk about some more effects. Absolutely. Well, there is a good reason like 
why it's been frozen. Uh it's because uh we need to maintain uh some um some um 
compatibility with uh plugins. And since plugins read and parse output of older 
commands, that's why we have like this uh Jurassic Park uh area in our uh 
codebase which we're very much willing to get rid of and that's where we need 
help um of community. also worth mentioning that transition uh so basically we 
we have this situation like with uh plugins um when we moved from go DAP uh to 
manage dependencies of CFCLI to uh go modules currently it's been pointed to 
the uh prev6 code uh and it's located in CF subfolder of CLI repository um and 
u in reality I think many uh currently like plug-in authors they just like have 
to uh find like different workarounds uh like using copy or shelling out uh 
instead of relying on this uh plug-in interface. Um also uh there is a bigger 
question about uh what is uh plug-in ecosystem for CFCLI. Right now we have 
repository also known as like clipper and uh we would like to better understand 
like like what what really like plugin being in this repository means. We don't 
do any security scanning of this repository uh like whitelisted uh plugins. uh 
we uh are not testing compatibility with older version of CLI with any versions 
of CLI or uh any uh Cloud Foundry CF CF deploy um versions. Some uh plugins 
functionality already been uh ported inside the modern like V8 CLI. Many 
plugins are broken. Um even uh talking about uh architecture uh so these days 
like many Linux machines like run on ARM um many uh Macs uh run on ARMS uh 
plugins they do not support this uh architecture. So uh building uh finally uh 
to be able to enable developers to build new plugins and modernize plugins uh 
our current uh documentation and directions like how to build or upgrade like 
they just like really u really out of date. So how can we rethink how we want 
to do the architecture? Um, the main thing for me was uh the ability to have a 
a consistent programmable output from it. So you'll be able to read the data no 
matter where you're at. Maybe it's JSON format, maybe it's YAML format, maybe 
it's XYZ format, doesn't really matter. We don't know what format to use. Some 
of the commands do it, some of the commands don't. We want to get some 
consistency there. The other thing is there's no hooks. What I mean by hooks is 
within the current commands, would you like to have things like um before you 
do a CF push, would you like to do a um an anti anti virus scan or before you 
create a a space, does your organization have a a naming convention to use, 
right? Things like that. So, we like to be able to put in hooks within the CLI, 
maybe pre and post operations to do it. The other thing is right now the uh the 
plugins are are generally written towards Go, right? It's it's all Go code. 
Well, it doesn't have to be Go code. It's just an executable. It could be a 
shell script. It doesn't really matter. Do we need something that could be more 
expansive than it is? Okay. Um the other thing is um we have life cycle 
management, but the one thing it doesn't do is upgrades. you don't know there's 
a new upgrade to the coming plugin and right now we only have um a place to put 
pullet plugins but we all we all know there are private plugins as well. So you 
need to have a life cycle management that you'll be able to specify how to 
upgrade plugins at some point. And also it'd be nice to be able to instead of 
looking at documents and copying code and cutting and paste and looking around 
have ability to generate a framework for the plugin itself. So if you're doing 
a a plug-in for hooks, it looks a little different. If you're doing a plug-in 
to to do things like um uh get information from your from your services, that 
can be a different type of style. And the the real question is, do we really 
need backward compatibility? Cuz this is a new level major version change. So, 
do we really care about backward compatibility? Like I said on my my slide, I 
like to break things. So, I don't think we need compatibility in the 
background, but again, that's not up to me. It's up to the community. All 
right. So the main takeaway is go to the Slack channel, go ahead and look at uh 
the CF plugins and provide us feedback so we can figure out what we want to do 
so we can create the necessary RFC's that we need they need to create and stay 
tuned. We'll start having um plug-in meetings so people who are interested 
interested stakeholders we can have a common meeting and come together and talk 
about this topic. Let's take a picture like that's direct link like to the 
Google document. Um, thank you very much for listening. Please uh take an 
action. Please uh participate and let's figure out like what to do with this 
plugins. Thank you. [Applause] 