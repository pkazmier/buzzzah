# Buzzzah

A simple game server that allows participants to join teams and "buzz in". I
built this to facilitate trivia matches I host for my team. Because these are
hosted via Zoom, it was always difficult to discern who "buzzed in" first,
second, third ... So I built buzzzah (buzzer spelled with a Boston accent),
which can be used to help facilitate running a game (not provided) that
requires users to buzz in.

https://github.com/pkazmier/buzzzah/assets/747855/a92507d7-7eb1-4be9-b3aa-aaadfda72316

## Installation

You'll need to [install Go](https://golang.org/doc/install).

```sh
go install github.com/pkazmier/buzzzah@latest
```

## Start a game

To start the server, you must specify the address and port to listen on as
well as the name of the team that hosts will join (you can have more than one
host). The host's web client will provide the ability to reset the buzzers and
edit the scoreboard.

```sh
buzzzah 0.0.0.0:8080 HostTeam
```

To join the game, participants and hosts should point their browsers to the
address provided above. They will be prompted for a name that will be used to
identify their bubble in the UI as well as a team name to which they are
attached. Hosts should join the special team name passed when starting up the
server.

Users can then press the "Buzz In" to signify they wish to buzz in. As each
user buzzes in, the UI is reflected to show the order in which users buzzed in
via a numeral next to their name as well as the size of the bubble. The
largest bubble is the first person to buzz in.

Hosts can then press the "Reset Buzzers" to reset the state of the users'
buzzers and to get ready for the next round. In addition, hosts can directly
edit the scoreboard table, which will then update all of the users
scoreboards.

## Credit

Both the client and server are based on the chat server
[example](https://github.com/nhooyr/websocket/tree/master/internal/examples/chat)
in nhooyr's excellent Go [websocket](https://github.com/nhooyr/websocket) package.

I used the wonderful
[D3](https://d3js.org) library to build the web UI.
