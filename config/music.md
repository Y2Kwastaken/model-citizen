# music.json

How songs are queued, downloaded and played.

```json
{
    "max_queue_length": 100,
    "cache_retention": 25,
    "prefetch_ahead": 2,
    "playback_timeout_seconds": 120,
    "volume": 0.3,
    "ducked_gain": 0.25
}
```

| Key | Default | Meaning |
| --- | --- | --- |
| `max_queue_length` | 100 | Songs a server's queue can hold. 0 or less is unlimited |
| `cache_retention` | 25 | Downloaded songs kept on disk, oldest evicted first. 0 or less keeps everything |
| `prefetch_ahead` | 2 | Upcoming songs downloaded in the background so they start instantly. 0 turns prefetch off |
| `playback_timeout_seconds` | 120 | Deadline for downloading a song and starting ffmpeg on it |
| `volume` | 0.3 | How loud songs play, above 0 and at most 1. Mastered music runs about 10dB louder than the bot's voice, so it's turned down |
| `ducked_gain` | 0.25 | How much of the music is left while the bot talks over it, 0 to 1 |

The cache folder is fixed at `/music`, because it has to match the
`music-provider` volume in `docker-compose.yml`.
