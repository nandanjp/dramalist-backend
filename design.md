## Overview

Although this application is about compiling the list of dramas and shows that I have and plan to watch from now and into the future, I want this application to have a long reaching vision - it should serve as a one stop replacement for applications like https://mydramalist.com/ and https://myanimelist.net/.

Of course, these applications are long standing; they have had millions of users over a long period of time and have solid green paths already laid out. However, the applications feel outdated from a UI standpoint, and from a latency and performance standpoint, there can be significant optimizations to really flesh things out. Here are some of the motivations and features that will make our application standout:

### Not just a Search Platform

Watching media and content should help people inform their opinions about what makes media "good"; what does a show or movie do that should compel the viewer to continue watching? What impact does the media have on the viewer. Collectively, why should one want to watch a show or movie in the first place?

Ultimately, these are questions that anyone who consumes media will receive from other people, and so it is the responsibility of the viewed to create consolidate and convey their thoughts to a potential audience.

As it stands, applications like myanimelist and mydramalist rely solely on the reviews written from other people to inform viewer decisions to watch shows. Although community lists and genres are general motivators to propel direction, there are more powerful tools available that can make these ideas discrete.

1. AI analysis over collective user reviews can generalize the type of media that appeals to them. This can fundamentally help other viewers choose the shows they watch, since they can better align on the value front, rather than just aligning with what a review has to say about a singular show.
2. Topics and genres can be used as sentiment analysis tools to really hone in on giving better recommendations to users, and helps refine searching capability. Furthermore, disimilar sentiments can serve as good indication for users to explore the media they watch and really dig deep into their interests

### A Clean UI

I would like this application to feel modern, to be clean, simple, sleek, and overall polished, compared to other offerrings that feel outdated. This can effectively be done by following modern design principles baked into component libraries like ShadCN, but also in following UI principles like clean animations, clean colour palletes, and other smooth patterns to make the application feel inviting overall.

### A Performant Application

The central focus of this app is to be scalable, distributed (if needed), and fast, focusing on latency and progressive design to enhance the feel of the application. We should opt for image optimizations to increase site load, should utilize tools like ElasticSearch to optimize searching ability (one poor feature of other applications), use caching to increase request-response route timings, and reduce the overall burden on single services by optimizing on the request path. The microservice architecture utilized will enable this to be possible in general, but we will make general optimizations described in the next section

### Conclusion

I would like this application to not only be a testament of modern UI and technology, but also to serve as a meaningful platform to allow people to make more informed decisions about the shows they watch, while also being intentional about how they recommend and converse about the media they consume. It is meant to drive conversation and, generally serve as a tool to aid in the overall process of media discovery along with purposeful media engagement (the central tenet is essentially WHY should we consume the media we do).

## Technical paths

The application should have a limited, well-defined and optimized set of green paths to ensure we are not doing too much and are doing what we do do really well. Here is general description of the green paths we should look to optimize and maintain:

1. Search: we should ensure that searching for media is as optimal as possible, including allowing for the possibility of searching for content in it's native language, finding lists of shows through genres, tags, release dates, episode counts, origin country, featured actors, etc. This is something that we should hone down on and should ensure we are constantly tapping into with the services being utilized.
2. User Driven Events: the core focus of the application are the users themselves. Users should be able to view their list of shows and their reviews as instantaneously as possible. Otherwise, there would be no need for a user to use this application over another service. This means keeping the show and review service as lean and low-latent as possible, but also ensures that we do the basics really well (caching when necessary, using progressive UI, keep payloads minimal etc.)
3. AI functionality: if AI services are slow, there will be no incentive to utilize it in the first place. We should make sure that we optimize on user queries and provide the bare minimum information, while being useful as a tool. Do not try to do more than what is necessary; AI is a tool to help the user quickly find information and does not need to be a long session driven beast.

### Actual Flows

Users on the platform should have the ability to do the following:

1. Search for any media in our catalog by name (in any language), genre, tag, origin country, created_at date and type. There should be clear filtering UI, search hints, and other selection tools to help streamline the search process.
2. Ditto 1 but for actors
3. User's should be able to view actor and show pages that provide clean, expressive and rich UI. We should make this a hallmark of the application.
4. User's can create reviews for any show in rich text.
5. Receive AI recommendations based on shows in their catalog and on genres, tags, ideas etc. that they are interested in.
6. User's should be able to view other user's lists to better expand on their catalog. Furthermore, they should be able to easily extract suggestions and recommendations based off the interests and overall motives of other users.
