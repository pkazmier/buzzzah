(() => {
  const getCookieValue = (name) =>
    document.cookie.match("(^|;)\\s*" + name + "\\s*=\\s*([^;]+)")?.pop() || "";

  const isHost = getCookieValue("host") === "true";

  class GameServer extends EventTarget {
    constructor(url) {
      super();
      this.url = url;
      this.conn = null;
      this.dial();
    }

    send(msg) {
      this.conn.send(JSON.stringify(msg));
    }

    dial() {
      this.conn = new WebSocket(this.url);

      this.conn.addEventListener("close", (ev) => {
        console.log(
          `WebSocket Disconnected code: ${ev.code}, reason: ${ev.reason}`
        );
        switch (ev.code) {
          case 1001:
            break;
          default:
            // send user back to login page on error
            window.location.replace("/");
            break;
        }
      });

      this.conn.addEventListener("open", (ev) => {
        console.log("websocket connected");
      });

      this.conn.addEventListener("message", (ev) => {
        if (typeof ev.data !== "string") {
          console.error("unexpected message type", typeof ev.data);
          return;
        }
        const msg = JSON.parse(ev.data);
        this.dispatchEvent(new CustomEvent(msg.type, { detail: msg.data }));
      });
    }
  }

  function buzzScale(count) {
    switch (count) {
      case 0:
        return 1;
      case 1:
        return 3;
      case 2:
        return 2.5;
      case 3:
        return 2.0;
      case 4:
        return 1.75;
      default:
        return 1.5;
    }
  }

  let width = 2560;
  let height = 1440;
  const default_radius = 40;
  const default_font_size = "22px";
  const default_stroke_width = 3;

  const color = d3.scaleOrdinal(d3.schemeCategory10);

  const simulation = d3
    .forceSimulation()
    .force("charge", d3.forceManyBody().strength(-2000))
    .force(
      "link",
      d3
        .forceLink()
        .id((d) => d.id)
        .distance(200)
    )
    .force(
      "collide",
      d3.forceCollide((d) => buzzScale(d.buzz) * (default_radius * 1.05))
    )
    .force("x", d3.forceX(0))
    .force("y", d3.forceY(0))
    .on("tick", ticked);

  const svg = d3
    .select("div#container")
    .append("svg")
    .attr("width", width)
    .attr("height", height)
    .attr("viewBox", [-width / 2, -height / 2, width, height])
    .attr("style", "max-width: 100%; height: auto;");

  const g = svg.append("g");

  svg.call(
    d3
      .zoom()
      .extent([
        [0, 0],
        [width, height],
      ])
      .scaleExtent([1 / 8, 8])
      .on("zoom", zoomed)
  );

  function zoomed({ transform }) {
    g.attr("transform", transform);
  }

  let link = g
    .append("g")
    .attr("stroke-width", default_stroke_width)
    .selectAll("line");

  let node = g
    .append("g")
    .attr("stroke-width", default_stroke_width)
    .selectAll(".node");

  function ticked() {
    node.attr("transform", (d) => `translate(${d.x}, ${d.y})`);

    link
      .attr("x1", (d) => d.source.x)
      .attr("y1", (d) => d.source.y)
      .attr("x2", (d) => d.target.x)
      .attr("y2", (d) => d.target.y);
  }

  let nodes = []; // list of { id: "Pete", team: "A", buzz: 0}
  let links = []; // list of { source: "Pete", target: "A" }

  function updateGraph() {
    simulation.nodes(nodes);
    simulation.force("link").links(links);
    simulation.alpha(1).restart();

    const t = svg.transition().duration(750).ease(d3.easeElastic);

    node = node
      .data(nodes, (d) => d.id)
      .join(
        (enter) =>
          enter
            .append("g")
            .attr("class", "node")
            .call((enter) =>
              enter
                .append("g")
                .attr("transform", "scale(1)")
                .call((enter) =>
                  enter
                    .append("circle")
                    .attr("r", default_radius)
                    .attr("fill", (d) => color(d.team))
                )
                .call((enter) =>
                  enter
                    .append("text")
                    .text((d) => (d.buzz ? `${d.buzz} ${d.id}` : d.id))
                    .attr("y", ".35em")
                    .attr("stroke-width", 0)
                    .attr("font-size", default_font_size)
                    .attr("text-anchor", "middle")
                )
                .call((enter) =>
                  enter
                    .transition(t)
                    .attr("transform", (d) => `scale(${buzzScale(d.buzz)})`)
                )
            ),
        (update) =>
          update.call((update) =>
            update
              .selectAll("g")
              .transition(t)
              .attr("transform", (d) => `scale(${buzzScale(d.buzz)})`)
              .selectAll("text")
              .text((d) => (d.buzz ? `${d.buzz} ${d.id}` : d.id))
          ),
        (exit) => exit.remove()
      );

    link = link
      .data(links, (d) => `${d.source.id}\t${d.target.id}`)
      .join("line");
  }

  const board = d3.select("table").style("opacity", 1);
  let teamNames = board.append("thead").append("tr").selectAll("th");
  let teamScores = board.append("tbody").append("tr").selectAll("td");

  function updateScores() {
    scores.sort((a, b) => a.team.localeCompare(b.team));
    const t = d3.transition().duration(1000);

    teamNames = teamNames
      .data(scores, (d) => d.team)
      .join("th")
      .text((d) => d.team);

    teamScores = teamScores
      .data(scores, (d) => d.team)
      .join(
        (enter) =>
          enter
            .append("td")
            .attr("contenteditable", isHost ? "true" : "false")
            .on("keydown", function (e, d) {
              if (e.key === "Enter") {
                const num = Number(this.innerText);
                if (!isNaN(num)) {
                  gs.send({
                    type: "score",
                    data: { team: d.team, score: num },
                  });
                  this.blur();
                  d3.select(this).classed("pending", false);
                } else {
                  e.preventDefault();
                }
              }
            })
            .text((d) => d.score),
        (update) =>
          update
            .transition(t)
            .style("opacity", (d) => (d.changed ? 0 : 1))
            .transition(t)
            .text((d) => d.score)
            .style("opacity", 1)
            .on("end", (d) => (d.changed = false))
      );
  }

  let scores = [
    // { team: "Perf", score: 0, changed: true },
    // { team: "Dev", score: 0, changed: true },
    // { team: "COE", score: 0, changed: true },
    // { team: "Eng", score: 0, changed: true },
  ];

  // Testing purposes
  // updateScores();
  // setTimeout(() => {
  //   scores = [
  //     { team: "Perf", score: 10, changed: true },
  //     { team: "Dev", score: 0, changed: true },
  //     { team: "COE", score: 0, changed: true },
  //     { team: "Eng", score: 0, changed: true },
  //   ];
  //   updateScores();
  // }, 3000);
  //
  // setTimeout(() => {
  //   scores = [
  //     { team: "Perf", score: 10, prior: 10 },
  //     { team: "Dev", score: 0, changed: true },
  //     { team: "COE", score: 20, changed: true },
  //     { team: "Eng", score: 0, changed: true },
  //   ];
  //   updateScores();
  // }, 6000);

  // Setup the Buzz In or Reset button
  const buzzer = d3.select("#buzzer");
  buzzer.text(isHost ? "Reset Buzzers" : "Buzz In");
  buzzer.on("click", () =>
    gs.send({ type: isHost ? "reset" : "buzzer", data: { name: "" } })
  );

  const gsURL = new URL(`ws://${location.host}/join`);
  gs = new GameServer(gsURL);

  gs.addEventListener("chat", (ev) => {
    console.log(`${ev.detail.name} sent ${ev.detail.text}`);
    updateGraph();
  });

  gs.addEventListener("state", (ev) => {
    const users = ev.detail.users;
    const buzzed = ev.detail.buzzed;
    const scoreboard = ev.detail.scoreboard;

    console.log("Received game state:", ev.detail);

    nodes = [];
    links = [];

    buzzOrder = {};
    for (let i = 0; i < buzzed.length; i++) {
      const name = buzzed[i];
      buzzOrder[name] = i + 1;
    }

    users.forEach((u) => {
      nodes.push({ id: u.name, team: u.team, buzz: buzzOrder[u.name] || 0 });
      if (!nodes.find((e) => e.id == u.team)) {
        nodes.push({ id: u.team, team: u.team, buzz: 0 });
      }
      links.push({ source: u.name, target: u.team });
    });

    for (let team in scoreboard) {
      scores.push({ team: team, score: scoreboard[team], changed: false });
    }

    updateGraph();
    updateScores();
  });

  gs.addEventListener("join", (ev) => {
    const name = ev.detail.name;
    const team = ev.detail.team;
    console.log(`${name} has join team ${team}`);

    if (!nodes.find((e) => e.id == name)) {
      nodes.push({ id: name, team: team, buzz: 0 });
    }
    if (!nodes.find((e) => e.id == team)) {
      nodes.push({ id: team, team: team, buzz: 0 });
      scores.push({ team: team, score: 0, changed: false });
    }
    if (!links.find((e) => e.source == name && e.target == team)) {
      links.push({ source: name, target: team });
    }

    updateGraph();
    updateScores();
  });

  gs.addEventListener("leave", (ev) => {
    const name = ev.detail.name;
    const team = ev.detail.team;
    console.log(`${name} has left team ${team}`);

    nodes = nodes.filter((e) => e.id != name);
    links = links.filter((e) => e.source.id != name);

    if (!links.find((e) => e.target.id == team)) {
      nodes = nodes.filter((e) => e.id != team);
    }

    if (!nodes.find((e) => e.team == team)) {
      scores = scores.filter((e) => e.team != team);
    }

    updateGraph();
    updateScores();
  });

  gs.addEventListener("buzzer", (ev) => {
    const name = ev.detail.name;
    console.log(`${name} buzzed in!`);

    // Find the index of the last one to buzz in
    const lastBuzzIn = nodes
      .map((e) => e.buzz)
      .reduce((a, b) => Math.max(a, b), 0);

    const node = nodes.find((e) => e.id == name);
    if (node) {
      node.buzz = lastBuzzIn + 1;
    }

    updateGraph();
  });

  gs.addEventListener("score", (ev) => {
    const team = ev.detail.team;
    const score = ev.detail.score;
    console.log(`score changed: ${team}=${score}`);

    scores.forEach((s) => {
      s.changed = false;
      if (s.team == team) {
        s.score = score;
        s.changed = true;
      }
    });

    updateScores();
  });

  gs.addEventListener("reset", (ev) => {
    console.log("resetting buzzers");
    nodes.forEach((e) => (e.buzz = 0));
    updateGraph();
  });
})();
