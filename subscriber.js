(() => {
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
        // TODO: Don't reconnect on 1011 internal server errors.
        // Instead popup an error message for user.
        if (ev.code !== 1001) {
          console.log("Reconnecting in 1s");
          setTimeout(() => this.dial(), 1000);
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
        return 4;
      case 2:
        return 3;
      case 3:
        return 2;
      default:
        return 1.5;
    }
  }

  let width = 1200;
  let height = 600;

  const color = d3.scaleOrdinal(d3.schemeCategory10);

  const simulation = d3
    .forceSimulation()
    .force("charge", d3.forceManyBody().strength(-1000))
    .force(
      "link",
      d3
        .forceLink()
        .id((d) => d.id)
        .distance(100)
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
    .attr("stroke", "#fff")
    .attr("stroke-width", 1.5)
    .selectAll("line");

  let node = g
    .append("g")
    .attr("stroke", "#fff")
    .attr("stroke-width", 1.5)
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
  let buzzcount = 0; //

  function update() {
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
                    .attr("r", 20)
                    .attr("fill", (d) => color(d.team))
                )
                .call((enter) =>
                  enter
                    .append("text")
                    .text((d) => d.id)
                    .attr("y", ".35em")
                    .attr("fill", "#fff")
                    .attr("stroke-width", 0)
                    .attr("font-size", "12px")
                    .attr("text-anchor", "middle")
                )
            ),
        (update) =>
          update.call((update) =>
            update
              .selectAll("g")
              .transition(t)
              .attr("transform", (d) => `scale(${buzzScale(d.buzz)})`)
          ),
        (exit) => exit.remove()
      );

    link = link
      .data(links, (d) => `${d.source.id}\t${d.target.id}`)
      .join("line");
  }

  d3.select("#buzzer").on("click", () =>
    gs.send({ type: "buzzer", data: { name: "" } })
  );

  // Connect to the GameServer and wire up events
  // passing the token from the login page which
  // was sent here via query params on the redirect.
  gs = new GameServer(`ws://${location.host}/join${location.search}`);

  gs.addEventListener("chat", (ev) => {
    console.log(`${ev.detail.name} sent ${ev.detail.text}`);
    update();
  });

  gs.addEventListener("state", (ev) => {
    const users = ev.detail.users;
    const buzzed = ev.detail.buzzed;
    const score = ev.detail.score;

    console.log("Received game state:", ev.detail);

    buzzIdx = {};
    for (let i = 0; i < buzzed.length; i++) {
      const name = buzzed[i];
      buzzIdx[name] = i + 1;
      buzzcount++; // update global buzzcount
    }

    users.forEach((u) => {
      nodes.push({ id: u.name, team: u.team, buzz: buzzIdx[u.name] || 0 });
      if (!nodes.find((e) => e.id == u.team)) {
        nodes.push({ id: u.team, team: u.team, buzz: 0 });
      }
      links.push({ source: u.name, target: u.team });
    });

    update();
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
    }
    if (!links.find((e) => e.source == name && e.target == team)) {
      links.push({ source: name, target: team });
    }

    update();
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

    update();
  });

  gs.addEventListener("buzzer", (ev) => {
    const name = ev.detail.name;
    buzzcount += 1;
    console.log(`${name} buzzed in: ${buzzcount}!`);

    const node = nodes.find((e) => e.id == name);
    if (node) {
      node.buzz = buzzcount;
    }

    update();
  });
})();