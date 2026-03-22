# constraints

- video quality metadata is capped at 49. anything above this returns error. PLAYA might auto reject resolution above 4k.
- prioritized direct stream. fallback to use transcoding doesnt work yet.
- operation time out when playing video can happen when the graphql url is not direct IP.
