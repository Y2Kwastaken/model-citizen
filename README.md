# model-citizen

A better version of model-citizen a hand crafted and well understood beauty.

This is a big v2 upgrade of the previous model-citizen repository the rewrite largely contains ground up design choices implemented by hand.
Originally this project was coded by AI in some pretty major areas. That said after getting something working I wanted to get my hands dirty when it
comes to audio encoding and translation it was an area I had very little knowledge or experience in. I'm hope this project can push me much closer to
knowledge in this area.

At it's heart model citizen is a project driven to create an interactive and customziable AI chatbot for your server. Everything from the personality
to the discord bot will provide some level of configuration I hope both you and I as a user will find satisfactory.

## Configuration

Music and voice settings live in a TOML file, read once at startup:

```sh
# edit config/model-citizen.toml, then restart
docker compose up -d --force-recreate
```

Every key is optional and the file ships with every default written out. Unknown keys stop the bot at startup, so
typos don't go unnoticed. Secrets such as `DISCORD_KEY` stay in `.env`. To use a different path, set `MODEL_CITIZEN_CONFIG`.
