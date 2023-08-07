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
        if (ev.code !== 1001) {
          console.log("Reconnecting in 1s");
          setTimeout(() => this.dial(), 1000);
        }
      });

      this.conn.addEventListener("open", (ev) => {
        console.log("websocket connected");
      });

      // This is where we handle messages received.
      this.conn.addEventListener("message", (ev) => {
        if (typeof ev.data !== "string") {
          console.error("unexpected message type", typeof ev.data);
          return;
        }
        const msg = JSON.parse(ev.data);
        switch (msg.type) {
          case "chat":
            this.dispatchEvent(new CustomEvent("chat", { detail: msg.data }));
            break;
          case "join":
            this.dispatchEvent(new CustomEvent("join", { detail: msg.data }));
            break;
          case "leave":
            this.dispatchEvent(new CustomEvent("leave", { detail: msg.data }));
            break;
          case "buzzer":
            this.dispatchEvent(new CustomEvent("buzzer", { detail: msg.data }));
            break;
          default:
            console.log("unsupported message type received", msg.type);
        }
      });
    }
  }

  let width = 1200;
  let height = 900;

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
    .attr("style", "max-width: 100%; height: auto;")
    .on("click", (d) => gs.send({ type: "buzzer", data: { name: "Darrah" } }));

  let link = svg
    .append("g")
    .attr("stroke", "#fff")
    .attr("stroke-width", 1.5)
    .selectAll("line");

  let node = svg
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

  let nodes = []; // list of { id: "Pete", team: "A"}
  let links = []; // list of { source: "Pete", target: "A" }

  // let nodes = [
  //   { id: "Pete", team: "A" },
  //   { id: "Darrah", team: "A" },
  //   { id: "Brian", team: "B" },
  //   { id: "Alan", team: "B" },
  //   { id: "Colm", team: "C" },
  //   { id: "Chris", team: "C" },
  //   { id: "A", team: "A" },
  //   { id: "B", team: "B" },
  //   { id: "C", team: "C" },
  // ];
  //
  // let links = [
  //   { source: "Pete", target: "A" },
  //   { source: "Darrah", target: "A" },
  //   { source: "Brian", target: "B" },
  //   { source: "Alan", target: "B" },
  //   { source: "Colm", target: "C" },
  //   { source: "Chris", target: "C" },
  // ];

  function update() {
    simulation.nodes(nodes);
    simulation.force("link").links(links);
    simulation.alpha(1).restart();

    const t = svg.transition().duration(750);

    node = node
      .data(nodes, (d) => d.id)
      .join(
        (enter) =>
          enter
            .append("g")
            .attr("class", "node")
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
                .attr("font-size", "12px")
                .attr("text-anchor", "middle")
            ),
        (update) => update,
        (exit) => exit.remove()
      );

    link = link
      .data(links, (d) => `${d.source.id}\t${d.target.id}`)
      .join("line");
  }

  // Connect to the GameServer and wire up events
  gs = new GameServer(`ws://${location.host}/join${location.search}`);

  gs.addEventListener("chat", (ev) => {
    console.log(`${ev.detail.name} sent ${ev.detail.text}`);
    update();
  });

  gs.addEventListener("buzzer", (ev) => {
    console.log(`${ev.detail.name} buzzed in!`);

    const name = ev.detail.name;

    update();
  });

  gs.addEventListener("join", (ev) => {
    console.log(`${ev.detail.name} has join team ${ev.detail.team}`);

    const name = ev.detail.name;
    const team = ev.detail.team;

    if (!nodes.find((e) => e.id == name)) {
      nodes.push({ id: name, team: team });
    }
    if (!nodes.find((e) => e.id == team)) {
      nodes.push({ id: team, team: team });
    }
    if (!links.find((e) => e.source == name && e.target == team)) {
      links.push({ source: name, target: team });
    }

    update();
  });

  gs.addEventListener("leave", (ev) => {
    console.log(`${ev.detail.name} has left team ${ev.detail.team}`);

    const name = ev.detail.name;
    const team = ev.detail.team;

    nodes = nodes.filter((e) => e.id != name);
    links = links.filter((e) => e.source.id != name);

    if (!links.find((e) => e.target.id == team)) {
      nodes = nodes.filter((e) => e.id != team);
    }

    update();
  });
})();
