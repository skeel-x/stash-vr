<p align="center">
  <img src="internal/static/icon.png" alt="Stash-VR logo" width="96" height="96">
</p>

<h1 align="center">Stash-VR (skeel-x fork)</h1>

Watch your [Stash](https://github.com/stashapp/stash) library in VR. Stash-VR sits between your Stash instance and your VR video player, so you can browse, play and manage your scenes from the player's own VR interface. Flat/2D videos work as well as VR videos.

This is an extended fork of [o-fl0w/stash-vr](https://github.com/o-fl0w/stash-vr). On top of the original it adds:

* **A web UI** - Players page with one-tap links and library status, Sections, Setup (every runtime option, stored in `config.json`) and Log pages.
* **Video rules** - an ordered table that maps Stash tags to projection, stereo layout, field of view, lens, eye swap, force mono and passthrough, shared by all players, with a scene inspector.
* **HereSphere profiles** - per-scene screen settings saved from the headset and handed back on any headset, rule profiles and generated profiles.
* **Passthrough** - alpha-packed and chroma-key masks, passthrough backgrounds.
* **Funscript variants** - alternate scripts next to the video or from the timestampTrade index show up in HereSphere's script picker.
* **Watch history** - watched state and resume position tags, resume on any headset.
* **Smart and auto sections** - Continue watching, Recommended for you, Recently added, Random and more, plus generated per-studio and per-performer sections, ordered and shown per player.
* **Release-date lookup** - missing scene dates looked up from your stash-boxes, optionally written back to Stash.
* **Performer facets** - `Country:` and `Age:` tags for filtering.
* **Playa** - native support for the [Playa VR video player](https://playavr.com/).

It pairs with the companion Stash plugin [vrQualityTags](https://github.com/skeel-x/vrQualityTags), which measures each file (projection, stereo layout, lens, alpha channel, resolution) and tags it; the default video rules react to those tags. Stash-VR works without it, but then formats have to come from your own tags and rules.

Credits: the original Stash-VR is by [o-fl0w](https://github.com/o-fl0w/stash-vr). The Playa integration was contributed by RGinting367.

## Quick start

### Docker

Images for linux/amd64 and linux/arm64 are published to `ghcr.io/skeel-x/stash-vr` (tags `latest` and per version).

```
docker run -d --name=stash-vr \
  -p 9666:9666 \
  -v /path/on/host/stash-vr:/config \
  -e STASH_GRAPHQL_URL=http://stash-host:9999/graphql \
  -e STASH_API_KEY=XXX \
  ghcr.io/skeel-x/stash-vr:latest
```

The `/config` directory holds `config.json`, stored profiles and caches; it must be writable by uid 65532 (the image's non-root user), for example `chown 65532:65532 /path/on/host/stash-vr`. `STASH_API_KEY` is only needed when Stash uses authentication. A compose file is in [docker-compose.yml](docker-compose.yml).

### Release binary

Download the binary for your platform (Linux amd64/arm64, Windows amd64, macOS arm64/amd64) from the [releases page](https://github.com/skeel-x/stash-vr/releases), verify it against the checksums file and run it:

```
chmod +x stash-vr_*_linux_amd64
./stash-vr_*_linux_amd64 --STASH_GRAPHQL_URL=http://stash-host:9999/graphql --STASH_API_KEY=XXX
```

Run it with `-h` to list all options. `config.json` is written to a `config` directory next to the binary unless `CONFIG_PATH` says otherwise.

### First steps

1. Open `http://<host>:9666` in a regular browser. The Players page shows whether Stash is reachable.
2. Go to **Setup** and check the Stash URL and API key, then look through the video rules and the other options. Changes apply immediately.
3. Optionally install [vrQualityTags](https://github.com/skeel-x/vrQualityTags) in Stash and run it so the default video rules have tags to act on.
4. Arrange sections on the **Sections** page.
5. In the headset, open the link for your player from the Players page (HereSphere, DeoVR) or add the shown address as a source in Playa.

### Playa notes

- Browsing and streaming work directly: visit the Stash-VR address in Playa and it is detected automatically.
- Saved filters show as categories along with the Stash tags. A synthetic `Random` category randomizes the video feed and can be combined with other categories.
- Direct stream is the default; transcoded (HLS) qualities also work, including with an API key.
- No trailers or previews, only thumbnails. No write-back to Stash (play count, favorites and so on).
- Very high bitrates (8K) can lag on phones. If you get timeouts, check the GraphQL URL and the container network.

---

## Supported video players

| | HereSphere | DeoVR | Playa |
|---|---|---|---|
| Browse sections (saved filters, smart and auto sections) | yes | yes | yes, as categories |
| Covers (WebP/GIF converted), heatmaps for interactive scenes | yes | yes | yes |
| Cover badges: quality, format, AR, duration, frame rate | yes | yes | yes |
| Direct and transcoded streams | yes | yes | yes, incl. HLS |
| Video rules: projection, stereo, lens, FOV | yes | yes | projection and stereo |
| Passthrough (alpha-packed), eye swap, screen profiles | yes | no (force mono shows as stereo off) | no |
| Markers | yes, two-way | shown | no |
| Tags, rating, favorites, o-count, organized | two-way | no | no |
| Play count, play duration, resume position saved to Stash | yes | no | no |
| Funscripts (with variants), subtitles | yes | no | no |
| Delete scenes | yes | no | no |

## Installation

See [Quick start](#quick-start) for Docker and release binaries. For all compose options see [docker-compose.yml](docker-compose.yml).

After installation open your endpoint (e.g. `http://localhost:9666`) in a regular browser to verify your setup.

Stash-VR listens on port `9666` by default. To change the local port, use docker port binding, e.g. `-p 9000:9666`, or set `LISTEN_ADDRESS=:9000` to listen on port `9000` instead.

Example: connect to Stash running on stash-host:9999 with api key XXX and listen on port 9000:

`stash-vr --STASH_GRAPHQL_URL=http://stash-host:9999/graphql --STASH_API_KEY=XXX --LISTEN_ADDRESS=:9000`

### Building from source

Requires Go (see `go.mod` for the version):

```
go build -o stash-vr ./cmd/stash-vr
```

### Reverse proxy under a sub-path

Stash-VR can be served under a path prefix such as `https://example.com/stashvr/`. Have the proxy strip the prefix and send it in `X-Forwarded-Prefix`; generated player links and page assets then carry it.

nginx:

```nginx
location /stashvr/ {
    proxy_pass http://127.0.0.1:9666/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Prefix /stashvr;
}
```

Caddy:

```caddyfile
handle_path /stashvr/* {
    reverse_proxy 127.0.0.1:9666 {
        header_up X-Forwarded-Prefix /stashvr
    }
}
```

If your proxy cannot send the header, set the prefix as `base_path` on the Setup page or with `BASE_PATH` instead.

### Settings and the web UI

Open Stash-VR in a browser (for example `http://localhost:9666`). The Players page shows whether Stash is reachable and gives one-tap links for HereSphere and DeoVR and the address for Playa. Under Details the page offers one random scene with a Shuffle button, linking to the scene in Stash, and lists the auto section count, release dates found, missing and unchecked, stored HereSphere profiles and how many video rules generate profiles. Smart sections (Continue watching, Recommended for you, Recently added, Random and more) can be switched on, ordered and shown or hidden per player (HereSphere, DeoVR, Playa) on the Sections page. **Setup** lets you change every runtime option; changes apply immediately and are stored in `config.json`. The **Log** page shows the last lines the service logged, filterable by level and text, with optional auto-refresh.

`config.json` lives in the directory given by `CONFIG_PATH` (default: a `config` directory next to the binary). In Docker the image sets `CONFIG_PATH=/config`; mount a directory that is writable by uid 65532 (the image's non-root user). The environment variables and flags below only seed the file on first start; after that the file is the source of truth. `LISTEN_ADDRESS`, `DISABLE_LOG_COLOR` and `DISABLE_REDACT` are process settings and stay flags.

### Configuration

* `STASH_GRAPHQL_URL`
  * **Required**
  * Url to your Stash graphql - something like `http://<stash.host>:<9999>/graphql`.
* `STASH_API_KEY`
  * Api key to your Stash if it's using authentication, otherwise not required.

<details>
<summary>More (click to expand)</summary>

* `CONFIG_PATH`
  * Directory for `config.json`. Default: `config` next to the binary.
* `LISTEN_ADDRESS`
  * Default: `:9666`. Address and port Stash-VR listens on. Process setting (flag or env), not stored in `config.json`.
* `LOG_LEVEL`
  * Default: `info`. One of `trace`, `debug`, `info`, `warn`, `error`. Runtime name: `log_level`.
* `DISABLE_LOG_COLOR`, `DISABLE_REDACT`
  * Default: `false`. Plain log output without colours; show API keys and URLs unredacted in the log (for debugging only). Process settings.
* `FAVORITE_TAG`
  * Default: `FAVORITE`
  * Name of tag in Stash to hold scenes marked as [favorites](#favorites) (will be created if not present). Leave empty to turn favorite sync off. Runtime name: `favorite_tag`.
* `EXCLUDE_SORT_NAME`
  * Default: `hidden`
  * Tags with this sort name will not be applied to videos or used for categorization.
* `HEATMAP_HEIGHT_PX`
  * Default: 0 (use height of heatmap)
  * Manually set height of all heatmaps. If not set, height of the heatmap retrieved from Stash will be used, currently 15 by default.
* `FORCE_HTTPS`
  * Default: `false`
  * Force Stash-VR to use HTTPS. Useful as a last resort attempt if you're having issues with Stash-VR behind a reverse proxy.
* `BASE_PATH`
  * Default: empty. Path prefix when a reverse proxy serves Stash-VR under a sub-path and does not send `X-Forwarded-Prefix`. Runtime name: `base_path`.
* `SMART_SECTION_SIZE`
  * Default: `50` (10 to 500). Number of scenes in each smart section. Runtime name: `smart_section_size`.
* `GENERATE_SUMMARY_IDS`
  * Default: `false`. Adds a hidden short id derived from each scene's summary tags, so scenes with the same tag set can be found together in HereSphere. Runtime name: `generate_summary_ids`.
* `DEOVR_AUTOLOAD`
  * Default: `true`
  * Answer DeoVR's browser on the front page with the library document. Runtime name: `deovr_autoload`.
* `PERFORMER_FACETS`
  * Default: `true`
  * Add `Country:` and `Age:` tags per performer to HereSphere's tag list. Runtime name: `performer_facets`.
* `DATE_LOOKUP`
  * Default: `true`
  * Look up missing scene release dates from the stash-boxes configured in Stash. Runtime name: `date_lookup`.
* `DATE_WRITEBACK`
  * Default: `false`
  * Also write dates found that way back to Stash. Runtime name: `date_writeback`.
* `AUTO_STUDIO_MIN`, `AUTO_PERFORMER_MIN`
  * Default: `0` (off)
  * Generate a section per studio or performer with at least this many scenes, up to 50 of each. Runtime names: `auto_studio_min`, `auto_performer_min`.
* `FUNSCRIPT_INDEX_PATH`
  * Default: empty (off)
  * Absolute path to the timestampTrade plugin's `funscript_index.sqlite`; scripts it lists for a scene are offered as alternates in HereSphere. Runtime name: `funscript_index_path`.
* `COVER_BADGE_QUALITY`, `COVER_BADGE_FORMAT`, `COVER_BADGE_PASSTHROUGH`, `COVER_BADGE_DURATION`, `COVER_BADGE_FRAMERATE`
  * Default: `true`, `false`, `true`, `true`, `true`
  * Draw the quality, format, passthrough, duration and frame rate [cover badges](#cover-badges). Runtime name: `cover_badges`, an object with the booleans `quality`, `format`, `passthrough`, `duration` and `framerate`.
* `video_rules`
  * File only, edited on the Setup page. The ordered tag-to-format rules table. "Reset to defaults" on the Setup page restores the default rules.

</details>

## Usage

Browse to `http://<host>:9666` using a supported video player. You'll be presented with your library within their respective native UI.

### Cover badges

Stash-VR can draw small labels in the bottom left corner of scene covers,
so the library grid in the headset shows them at a glance. The top left
corner is left free for the icons HereSphere draws there. Each kind is
switched on or off under Cover badges on the Setup page:

* **Quality** (on by default): the tier tag the vrQualityTags plugin sets
  (`8K` in gold, `7K` in silver, `6K HBR` in bronze; the `HQ` parent tag is
  ignored). A scene without a tier tag shows the resolution of its file
  instead, named by width from 4K up (`5K`, `6K`) and by height below
  (`1080p`), in grey.
* **Format** (off by default): the projection the video rules resolve to:
  `180`, `360`, `FISHEYE` (with the field of view when a rule sets one, for
  example `FISHEYE 200`) or `FLAT 3D`. Flat 2D scenes get no label.
* **Passthrough** (on by default): `AR` on scenes whose rules turn on
  passthrough (an alpha matte) or a chroma-key mask.
* **Duration** (on by default): the running time of the scene's first
  file, in minutes below an hour (`42 min`) and in hours and minutes from
  one hour (`1 h 05`), in grey.
* **Frame rate** (on by default): the frame rate of the first file,
  rounded to whole frames (`60 fps` for 59.94, `30 fps` for 29.97), in
  grey. Left out when Stash does not know it.

Badges are drawn in that order, left to right; those that do not fit the
cover width are left out.

Badges are 7% of the cover height (at least 18 px), inset by 2% of the
cover width, and sit just above the heatmap strip of interactive scenes. Covers with badges are re-encoded as
JPEG and the last 500 are kept in memory; covers without any are served as
Stash sends them. Headsets keep covers for a day, so while any badge is on
the cover URLs carry a short fingerprint of the badge settings and layout
(`?b=...`), and changing them shows on the next library load.

Under the checkboxes the Setup page previews a real cover with the badges
as ticked, before saving, redrawn on every change. It picks a scene with a
tier tag and, if Stash has one, the `Alpha` tag, so every kind of badge
shows; enter a scene id and press Show to preview another scene. The
preview uses the saved video rules and leaves the saved settings and the
rendered cover cache alone.

### HereSphere

##### Two-way sync

To enable two-way sync with Stash the relevant toggles (`Overwrite tags` etc.) in the cogwheel at the bottom right of preview view in HereSphere needs to be on.

#### Manage metadata

Scene metadata is handled using `Video Tags` in HereSphere. Both for presentation and making changes.

* Scene tags
  * `#:<Name>`
  * To tag a scene, create a tag in HereSphere following above format.
    * `#:Music` will apply the tag `Music`, creating it if necessary.
  * To untag a scene, delete the tag in HereSphere.
  * Name may not start with `#`
    * ~~`#:#Music`~~
* Parent tags
  * `#<Parent>:<Name>` - read-only.
  * Hidden in tag editor, visible in categorized tags under category `#<Parent>`
* Ancestor tags
  * `#:#<Name>` - read-only
  * Hidden in tag editor, visible in categorized tags under `#` as `#<Name>`
* Summary tag
  * `Summary:<SUMMARY>` - read-only
  * Generated summary string of the scene, parent and ancestor tags shown above seekbar.
* Studio
  * `Studio:<Name>` - read-only.
* Performers
  * `@:<Name>` - read-only.
* Groups
  * `%:<Name>` - read-only.
* Play count
  * `Played:<Count>`
  * Automatically incremented when "logged in"
    * Uses `Minimum Play Percent` from Stash if set.
  * Pausing or closing a video also saves the play duration and the resume position to Stash (positions in the first 5 seconds or past 97% count as finished).
  * To decrement (delete last timestamp), delete the `Played` tag
* O-Count
  * `O-Count:<Count>`
  * To increment, add a tag `/o`
  * To decrement (delete last timestamp), delete the `O-Count` tag
* Organized
  * `Organized:<bool>`
  * To set organized, add a tag `/org`
  * To unset organized, delete the `Organized` tag
* Rating
  * `Rating:<value>`
  * Ratings set in HereSphere will be converted to its equivalent in Stash (4.5 stars => 90).
  * To unset rating, delete the `Rating` tag
* Read-only information
  * `Watched:yes|no`, `Resume:<mm:ss>`, `Released:<date>`, `Interactive:true`, `Resolution:<value>`, and the performer facets `Country:<name>`, `Country:<code>` and `Age:<years>`. Deleting or editing them in HereSphere changes nothing in Stash.
* Markers
  * A tag that is not one of the above becomes a marker when it has a time span (a start after 0 or an end) or is an existing marker HereSphere sends back. An untimed tag with an unknown prefix is ignored, and so are prefixes older Stash-VR versions used (`P:`, `Org:`, `movie:` and so on), which HereSphere may still keep in its cache.
  * `<Primary Tag Name>` (empty title)
    * `Music` will create marker with tag `Music` spanning HereSphere tag length
  * `<Primary Tag Name>:<Title>`
    * `Music:Solo` will create marker with tag `Music` and title `Solo` spanning HereSphere tag length
  * Set the start and end time using HereSphere controls.
  * Changes in HereSphere will sync to Stash

Changes reflect in HereSphere when videos are re-opened.

#### Favorites

When the favorite-feature of HereSphere is first used Stash-VR will create a tag in Stash named according to `FAVORITE_TAG` (the Setup page or env, defaults to `FAVORITE`) and apply that tag to your scene. With an empty favorite tag, favorites are not synced.

#### Release dates, watched state and auto sections

Scenes without a date in Stash get one from your stash-boxes (stashdb,
ThePornDB and others configured in Stash): a background job asks them by
fingerprint, one scene every few seconds, and accepts a result only when
it is the only match or its title matches the scene. Answers are kept in
`dates.json` next to `config.json`. Found dates show as `Released:` tags,
feed HereSphere's release date and the `Age:` facet, and can optionally be
written back to Stash (`date_writeback`); only dates whose title matches the
scene are written, never a single fuzzy match. A stash-box that errors is
asked again after six hours, a scene with no answer after 30 days.

HereSphere also gets `Watched:yes`/`Watched:no` and, for scenes with a
saved position, `Resume:12:34` tags. With `auto_studio_min` or
`auto_performer_min` set, studios and performers with enough scenes become
sections of their own; they show on the Sections page with an "auto" badge
and can be ordered, renamed or hidden like any other section.

#### Recommended for you

The "Recommended for you" smart section suggests scenes you have not played
yet, based on what you watched. It is on by default and, on an installation
that already has a saved section order, first appears right after Continue
watching so the section HereSphere opens on stays the same.

* History: scenes played in the last 90 days, plus scenes with an o-count
  or a rating of 80 or more from the same period (all time when fewer than
  10 scenes were played in it). Each counts 1, plus 1 if watched to the
  end, plus its o-count (at most 3), plus (rating - 60) / 20 above a
  rating of 60, halved for every 30 days since it was last played.
* Features: a scene's tags, performers (counted double) and studio, each
  weighted by how rare it is in the library (inverse document frequency).
  Tags with the excluded sort name and the format and quality tags of the
  vrQualityTags plugin (DOME, SBS, 8K, `VRP:` and the like) are ignored.
* Candidates are scenes never played, with a file, of the same kind as
  most of the history: VR scenes (tagged DOME, SPHERE or FISHEYE, or at
  least 3840 wide in 2:1 or 1:1) if most of what you watched was VR,
  otherwise flat scenes. Each scores how much its features overlap the
  history, divided by the square root of its feature count so heavily
  tagged scenes do not win by volume; the best `smart_section_size` are
  shown, newest first on equal scores.

The whole library is fetched in one query and scored in Stash-VR; the
result is kept for 30 minutes. With nothing watched yet the section is
left out.

#### Performer facets

Each performer's country appears twice, by English name and by code
(`Country:Sweden` and `Country:SE`, so either filter works), and each
performer's age as `Age:<years>`, so scenes can be filtered by them, for
example `@:Performer Name` together with `Age:30`. The age is taken at the
scene's release date (from Stash, or looked up from a stash-box), else today. Switch the facets off with
`performer_facets` on the Setup page.

#### Video rules and profiles

The Setup page has a rules table that maps Stash tags to how a scene is
shown: projection (equirectangular, 360, fisheye, cubemap, flat), stereo
layout, field of view, lens and passthrough. Rules apply top to bottom and
later rules override earlier ones. The defaults react to the tags the
[vrQualityTags](https://github.com/skeel-x/vrQualityTags) Stash plugin
measures from the file itself: DOME, SPHERE, FISHEYE, MKX200, RF52,
CUBEMAP, EAC, FLAT, SBS and TB, the MKX220 and VRCA220 lenses (fisheye,
220 degrees), MONO (stereo mono), RL (right eye first: eye swap), and
Alpha, which switches passthrough on for alpha-packed videos, and Chroma
Key, which gives chroma-keyed videos a generated HereSphere profile with a
passthrough background and a chroma-key mask (adjust the key colour in the
headset and save to keep it). Labels that
studios or stash-boxes attach (Passthrough, Augmented Reality, 180°, 360°)
no longer change the format on their own. All three players read the same
table.

A config written by an earlier version keeps its own rules, except that
the old default rules for the Passthrough, Augmented Reality, 180° and
360° labels are dropped automatically on start when they are unchanged
(the log names them); a rule you edited is kept. "Add missing
default rules" appends the defaults whose tag the table lacks and leaves
the existing rules as they are. "Add a preset" appends a filled rule card
(RF52 190, MKX200 200, MKX220 220, VRCA220 220, Passthrough (alpha
matte) on the Alpha tag with the passthrough background and alpha-packed
mask, Flat 2D) to adjust before
saving.

Each rule can also turn eye swap and force mono on or off. HereSphere gets
them through a generated profile; DeoVR has no eye swap and shows force
mono as stereo off.

HereSphere is asked to write its per-scene profile back. When you adjust
the screen (distance, position, scale, background) and save it with the
save icon next to "Global Settings" in HereSphere's video settings,
Stash-VR stores the profile under `<config>/hsp/` and hands it back the
next time the scene opens, on any headset. A rule can name one of these
scenes as its profile, so every scene with that tag opens with the same
geometry until it has a profile of its own.

Rules can also carry the screen settings themselves: position, rotation,
zoom, pan, origin, background (global, colour, passthrough) and mask
(none, alpha packed, chroma key), under "Screen and background" on each
rule card. Stash-VR then builds a HereSphere profile for every matching
scene that has no saved profile of its own. The values use HereSphere's
own units; "Copy from a saved profile" fills them from a scene you have
tuned and saved in the headset. Earlier versions of saved profiles are
kept in `hsp/history`. Generated profiles carry Stash's resume position
and last played time, so a scene resumes where it was left on any headset.

The scene inspector under the rules takes a scene id or part of a title
and shows, for up to five scenes, which rules match (by position and tag),
what they resolve to and where the HereSphere profile comes from: the
scene's own, a rule's saved profile, generated, or none, with its link.
An id is always looked up; titles are matched against scenes a player has
already loaded, so the search never queries the whole library.

The format coverage panel below it (press "Check format coverage") shows how
many scenes carry each tag the vrQualityTags plugin manages, each count
linking to that tag's scene list in Stash. It also counts VR-shaped scenes
(at least 3840 wide, about 2:1 or 1:1) that have no projection tag and lists
the first 50 with links, so a scene the plugin missed is easy to find.

#### Funscript variants

Stash-VR lists every funscript it finds next to a scene's video in
HereSphere's script picker. `<video>.funscript` is "Standard"; a file
named `<video>.ai.funscript`, `<video>_v2.funscript` or
`<video> alternate 1.funscript` shows as "AI", "V2" or "Alternate 1".
Multi-axis companion scripts (`.roll`, `.pitch`, `.twist`, `.sway`,
`.surge`, `.L1`, `.L2`, `.R0`-`.R2`) are not listed.

If you use the timestampTrade plugin, set `funscript_index_path` in Setup
(or `FUNSCRIPT_INDEX_PATH`) to its `funscript_index.sqlite`; scripts it
lists for a scene appear as "Alternate 1", "Alternate 2", ... with the
creator's name when known. Copies identical to a script already listed
are skipped. Variants are rescanned every five minutes or on Rebuild index.

### DeoVR

Open the Stash-VR address (for example `http://<host>:9666`) in DeoVR's browser; the library loads directly. If it does not appear, open the `/deovr` address shown on the Players page instead. Requests from DeoVR's browser to the front page receive the library document rather than the web page; turn this off with the `deovr_autoload` setting on the Setup page if you want the web page in DeoVR.

## Known issues/Missing features

### Unsupported filter types

* Premade Filters (i.e., Recently Released Scenes etc.) from Stash front page are not supported.
  * Tip: If you really want such filters to show they can easily be recreated and saved using regular filters in Stash.

### Very large libraries

Current HereSphere versions handle large libraries (tested with about 13,000 scenes in 50,000 section entries). If a player struggles, show fewer sections to it: untick them for that player on the Sections page, or lower `smart_section_size`.

### Missing thumbnail images in DeoVR

DeoVR won't show images served through HTTP - seems they only allow HTTPS. See <https://github.com/xbapps/xbvr/issues/1705>

## Troubleshooting

* If your Stash requires an api key, make sure you provide it to Stash-VR

* Make sure Stash-VR has network access to Stash
* Make sure your VR-headset has network access to both Stash and Stash-VR
  * Links handed to the player are built from the address the headset used to reach Stash-VR. Behind a reverse proxy, send `X-Forwarded-Proto` (or set `FORCE_HTTPS`) and, under a sub-path, `X-Forwarded-Prefix` (or set `base_path`).
* The Players page (Details) shows whether this device can load covers, and the **Log** page shows recent errors.
* A scene shown in the wrong shape: look it up in the scene inspector on the Setup page to see which rules matched; the format coverage panel lists VR-shaped scenes without a projection tag.
* If you can't seek or get errors about unsupported encoding/format, set the `Encoding` drop-down (above the seekbar) in HereSphere to `direct`. There's also an equivalent setting in DeoVR.
